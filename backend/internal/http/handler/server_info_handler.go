package handler

import (
	"net/http"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/versioninfo"
)

type ServerInfoHandler struct{}

func NewServerInfoHandler() *ServerInfoHandler {
	return &ServerInfoHandler{}
}

// GetInfo godoc
// @Summary Get server info
// @Description Returns stable server metadata for remote clients.
// @Tags system
// @Produce json
// @Success 200 {object} api.ServerInfoEnvelope
// @Router /info [get]
func (h *ServerInfoHandler) GetInfo(w http.ResponseWriter, _ *http.Request) {
	info := versioninfo.Current()
	writeDataJSON(w, http.StatusOK, api.ServerInfoResponse{
		Version:    info.Version,
		Commit:     info.Commit,
		BuildDate:  info.BuildDate,
		APIVersion: info.APIVersion,
	})
}
