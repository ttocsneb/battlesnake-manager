package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ttocsneb/battlesnake-manager/docker"
)

func registerBattleSnakeRoutes(r *mux.Router) {
	r.HandleFunc("/bs/{id}", battleSnakePoxyHandler("/"))
	r.HandleFunc("/bs/{id}/", battleSnakePoxyHandler("/"))

	r.HandleFunc("/bs/{id}/start", battleSnakePoxyHandler("/start/"))
	r.HandleFunc("/bs/{id}/start/", battleSnakePoxyHandler("/start/"))

	r.HandleFunc("/bs/{id}/move", battleSnakePoxyHandler("/move/"))
	r.HandleFunc("/bs/{id}/move/", battleSnakePoxyHandler("/move/"))

	r.HandleFunc("/bs/{id}/end", battleSnakePoxyHandler("/end/"))
	r.HandleFunc("/bs/{id}/end/", battleSnakePoxyHandler("/end/"))
}

func ensureContainerRunning(w http.ResponseWriter, r *http.Request, id string) (string, bool) {
	err := docker.EnsureContainerRunning(id)
	if err != nil {
		if err == docker.ErrorNotRegistered {
			notFound(w, r)
			return "", false
		}
		logError(w, r, "Could not start container", err)
		return "", false
	}

	// ignore the error since it can never fail after ensuring the container is running
	state, _ := docker.GetState(id)
	return state.IPAddress, true
}

func battleSnakePoxyHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := "bs-" + mux.Vars(r)["id"]
		ip, running := ensureContainerRunning(w, r, id)
		if !running {
			return
		}

		// Proxy the request to the battle snake
		req, err := http.NewRequest(r.Method, fmt.Sprintf("http://%v%v", ip, path), r.Body)
		if err != nil {
			logError(w, r, "Could not create pass-through request", err)
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logError(w, r, "Could not perform pass-through request", err)
			return
		}
		defer resp.Body.Close()

		// Respond to the original request with the proxied response
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			logError(w, r, "Could not write proxied response", err)
			return
		}

		// Let the docker job know that this battle snake has just been used
		go func() {
			docker.UpdateUsed(id)
		}()
	}
}
