package handlers

import (
	"net/http"

	frontendstatic "github.com/FKouhai/rss-frontend/static"
)

func staticHandler() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(frontendstatic.FS)))
}
