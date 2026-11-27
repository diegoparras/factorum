package app

import (
	"github.com/diegoparras/factorum/internal/avisoia"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/extractor"
)

// ArmarVision arma el modelo de visión de la configuración, o nil si no se puede usar: sin modelo
// configurado o, si es una IA externa, sin el aviso aceptado por un superadmin para ese host y esa
// versión del aviso (fail-closed: así no la usan ni la pantalla, ni los canales, ni MCP).
func ArmarVision(c *config.Config) *extractor.Vision {
	if !avisoia.EnUso(c) {
		return nil
	}
	return &extractor.Vision{BaseURL: c.LLMBaseURL, APIKey: c.LLMAPIKey, Modelo: c.LLMModelo, Timeout: c.LLMTimeout}
}
