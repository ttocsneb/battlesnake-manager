package api

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

func Serve() error {
	r := mux.NewRouter()

	registerBattleSnakeRoutes(r)
	registerGithubHandlers(r)

	return http.ListenAndServe(":80", r)
}

func logError(w http.ResponseWriter, r *http.Request, message string, err error) {
	fmt.Printf("ERROR while processing %v\n", r.URL.Path)
	fmt.Printf("\t%v\n", message)
	fmt.Printf("\t%v\n", err)

	w.WriteHeader(500)
	w.Write([]byte("500 Internal Server Error"))
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(404)
	w.Write([]byte("404 Battle-Snake Not Found"))
}
