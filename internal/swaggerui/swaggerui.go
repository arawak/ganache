package swaggerui

import (
	"net/http"

	swgui "github.com/swaggest/swgui/v5"
)

// Handler returns a Swagger UI handler (assets embedded, no CDN).
func Handler(specPath string) http.Handler {
	return swgui.New("Ganache API", specPath, "/swagger")
}
