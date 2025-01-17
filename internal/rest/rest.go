package rest

import (
	"context"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/lxc/lxd/lxd/request"
	"github.com/lxc/lxd/lxd/response"
	"github.com/lxc/lxd/lxd/util"
	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/logger"

	"github.com/canonical/microcluster/cluster"
	"github.com/canonical/microcluster/internal/rest/access"
	"github.com/canonical/microcluster/internal/rest/client"
	internalState "github.com/canonical/microcluster/internal/state"
	"github.com/canonical/microcluster/rest"
)

func handleAPIRequest(action rest.EndpointAction, state *internalState.State, w http.ResponseWriter, r *http.Request) response.Response {
	trusted := r.Context().Value(request.CtxAccess)
	if trusted == nil {
		return response.Forbidden(nil)
	}

	trustedReq, ok := trusted.(access.TrustedRequest)
	if !ok {
		return response.Forbidden(nil)
	}

	if !trustedReq.Trusted && !action.AllowUntrusted {
		return response.Forbidden(nil)
	}

	if action.Handler == nil {
		return response.NotImplemented(nil)
	}

	if action.AccessHandler != nil {
		// Defer access control to custom handler.
		accessResp := action.AccessHandler(state, r)
		if accessResp != response.EmptySyncResponse {
			return accessResp
		}
	} else if !action.AllowUntrusted {
		// Set the default access handler if one isn't specified.
		action.AccessHandler = access.AllowAuthenticated
		accessResp := action.AccessHandler(state, r)
		if accessResp != response.EmptySyncResponse {
			return accessResp
		}
	}

	if action.ProxyTarget {
		return proxyTarget(action, state, r)
	}

	return action.Handler(state, r)
}

func proxyTarget(action rest.EndpointAction, s *internalState.State, r *http.Request) response.Response {
	if r.URL == nil {
		return action.Handler(s, r)
	}

	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		logger.Warnf("Failed to parse query string %q: %v", r.URL.RawQuery, err)
	}

	var target string
	if values != nil {
		target = values.Get("target")
	}

	if target == "" || target == s.Name() {
		return action.Handler(s, r)
	}

	var targetURL *api.URL
	err = s.Database.Transaction(s.Context, func(ctx context.Context, tx *sql.Tx) error {
		clusterMember, err := cluster.GetInternalClusterMember(ctx, tx, target)
		if err != nil {
			return fmt.Errorf("Failed to get cluster member for request target name %q: %w", target, err)
		}

		targetURL = api.NewURL().Scheme("https").Host(clusterMember.Address).Path(r.URL.Path)

		return nil
	})
	if err != nil {
		return response.BadRequest(err)
	}

	clusterCert, err := s.ClusterCert().PublicKeyX509()
	if err != nil {
		return response.InternalError(fmt.Errorf("Failed to parse cluster certificate for request: %w", err))
	}

	client, err := client.New(*targetURL, s.ServerCert(), clusterCert, false)
	if err != nil {
		return response.InternalError(fmt.Errorf("Failed to get a client for the target %q at address %q: %w", target, targetURL.String(), err))
	}

	// Update request URL.
	r.RequestURI = ""
	r.URL.Scheme = targetURL.URL.Scheme
	r.URL.Host = targetURL.URL.Host
	r.Host = targetURL.URL.Host

	logger.Info("Forwarding request to specified target", logger.Ctx{"source": s.Name(), "target": target})
	resp, err := client.MakeRequest(r)
	if err != nil {
		return response.SmartError(fmt.Errorf("Failed to send request to target %q: %w", target, err))
	}

	return response.SyncResponse(true, resp.Metadata)
}

func handleDatabaseRequest(action rest.EndpointAction, state *internalState.State, w http.ResponseWriter, r *http.Request) response.Response {
	trusted := r.Context().Value(request.CtxAccess)
	if trusted == nil {
		return response.Forbidden(nil)
	}

	trustedReq, ok := trusted.(access.TrustedRequest)
	if !ok {
		return response.Forbidden(nil)
	}

	if !trustedReq.Trusted {
		return response.Forbidden(nil)
	}

	if action.Handler == nil {
		return response.NotImplemented(nil)
	}

	// If the request is a POST, then it is likely from the dqlite dial function, so hijack the connection.
	if r.Method == "POST" {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return response.InternalError(fmt.Errorf("Webserver does not support hijacking"))
		}

		conn, _, err := hijacker.Hijack()
		if err != nil {
			return response.InternalError(fmt.Errorf("Failed to hijack connection: %w", err))
		}

		state.Database.Accept(conn)
	}

	return action.Handler(state, r)
}

// HandleEndpoint adds the endpoint to the mux router. A function variable is used to implement common logic
// before calling the endpoint action handler associated with the request method, if it exists.
func HandleEndpoint(state *internalState.State, mux *mux.Router, version string, e rest.Endpoint) {
	url := "/" + version
	if e.Path != "" {
		url = filepath.Join(url, e.Path)
	}

	route := mux.HandleFunc(url, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Actually process the request.
		var resp response.Response

		// Return Unavailable Error (503) if daemon is shutting down, except for endpoints with AllowedDuringShutdown.
		if state.Context.Err() == context.Canceled && !e.AllowedDuringShutdown {
			err := response.Unavailable(fmt.Errorf("Daemon is shutting down")).Render(w)
			if err != nil {
				logger.Error("Failed to write HTTP response", logger.Ctx{"url": r.URL, "err": err})
			}

			return
		}

		if !e.AllowedBeforeInit {
			if !state.Database.IsOpen() {
				err := response.Unavailable(fmt.Errorf("Daemon not yet initialized")).Render(w)
				if err != nil {
					logger.Error("Failed to write HTTP response", logger.Ctx{"url": r.URL, "err": err})
				}

				return
			}
		}

		// If the request is a database request, the connection should be hijacked.
		handleRequest := handleAPIRequest
		if e.Path == "database" {
			handleRequest = handleDatabaseRequest
		}

		trusted, err := authenticate(state, r)
		if err != nil {
			resp = response.Forbidden(fmt.Errorf("Failed to authenticate request: %w", err))
		} else {
			r = r.WithContext(context.WithValue(r.Context(), any(request.CtxAccess), access.TrustedRequest{Trusted: trusted}))

			switch r.Method {
			case "GET":
				resp = handleRequest(e.Get, state, w, r)
			case "PUT":
				resp = handleRequest(e.Put, state, w, r)
			case "POST":
				resp = handleRequest(e.Post, state, w, r)
			case "DELETE":
				resp = handleRequest(e.Delete, state, w, r)
			case "PATCH":
				resp = handleRequest(e.Patch, state, w, r)
			default:
				resp = response.NotFound(fmt.Errorf("Method '%s' not found", r.Method))
			}
		}

		// Handle errors.
		if e.Path != "database" {
			err := resp.Render(w)
			if err != nil {
				err := response.InternalError(err).Render(w)
				if err != nil {
					logger.Error("Failed writing error for HTTP response", logger.Ctx{"url": url, "error": err})
				}
			}
		}
	})

	// If the endpoint has a canonical name then record it so it can be used to build URLS
	// and accessed in the context of the request by the handler function.
	if e.Name != "" {
		route.Name(e.Name)
	}
}

// authenticate ensures the request certificates are trusted before proceeding.
// - Requests over the unix socket are always allowed.
// - HTTP requests require our cluster cert, or remote certs.
func authenticate(state *internalState.State, r *http.Request) (bool, error) {
	if r.RemoteAddr == "@" {
		return true, nil
	}

	if state.Address().URL.Host == "" {
		logger.Info("Allowing unauthenticated request to un-initialized system")
		return true, nil
	}

	var trustedCerts map[string]x509.Certificate
	switch r.Host {
	case state.Address().URL.Host:
		trustedCerts = state.Remotes().CertificatesNative()
	default:
		return false, fmt.Errorf("Invalid request address %q", r.Host)
	}

	if r.TLS != nil {
		for _, cert := range r.TLS.PeerCertificates {
			trusted, fingerprint := util.CheckTrustState(*cert, trustedCerts, nil, false)
			if trusted {
				remote := state.Remotes().RemoteByCertificateFingerprint(fingerprint)
				if remote == nil {
					// The cert fingerprint can no longer be matched back against what is in the truststore (e.g. file
					// was deleted), so we are no longer trusted.
					return false, nil
				}

				return true, nil
			}
		}
	}

	return false, nil
}
