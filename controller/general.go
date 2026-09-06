package controller

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/repo"
	"OpenSPMRegistry/utils"
	"net/http"
)

type Controller struct {
	config       config.ServerConfig
	repo         repo.Repo
	timeProvider utils.TimeProvider
}

func NewController(config config.ServerConfig, repo repo.Repo) *Controller {
	return &Controller{
		config:       config,
		repo:         repo,
		timeProvider: utils.NewRealTimeProvider(),
	}
}

func (c *Controller) MainAction(w http.ResponseWriter, r *http.Request) {
	printCallInfo("MainAction", r)
	// 404 if no route matches
	writeErrorWithStatusCode("Not found", w, http.StatusNotFound)
}

func (c *Controller) StaticAction(w http.ResponseWriter, r *http.Request) {
	printCallInfo("FavIcon", r)
	http.ServeFile(w, r, "static"+r.URL.Path)
}

// OpenAPISpecAction serves the vendored OpenAPI spec for the registry API
// (see openapi/registry.openapi.yaml; sourced from swiftlang/swift-package-manager).
func (c *Controller) OpenAPISpecAction(w http.ResponseWriter, r *http.Request) {
	printCallInfo("OpenAPISpec", r)
	w.Header().Set("Content-Type", "application/yaml")
	http.ServeFile(w, r, "openapi/registry.openapi.yaml")
}

// DocsAction serves a Swagger UI page for browsing the OpenAPI spec served at OpenAPISpecAction.
func (c *Controller) DocsAction(w http.ResponseWriter, r *http.Request) {
	printCallInfo("Docs", r)
	http.ServeFile(w, r, "static/swagger.html")
}
