package handlers

import (
	"errors"
	"strings"
	"truckapi/internal/oauthissuer"

	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"
)

// tokenRequest is the OAuth 2.0 client_credentials token request body. We
// accept both application/json (what CHRob's own token endpoint uses) and
// application/x-www-form-urlencoded (RFC 6749 default) since we don't yet know
// which their callback issuer will use.
type tokenRequest struct {
	ClientID     string `json:"client_id" form:"client_id"`
	ClientSecret string `json:"client_secret" form:"client_secret"`
	GrantType    string `json:"grant_type" form:"grant_type"`
	// audience accepted-and-ignored so CHRob's standard payload doesn't
	// trip our parser if they include one.
	Audience string `json:"audience" form:"audience"`
}

type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// OAuthTokenHandler implements POST /oauth/token for CHRob to fetch a
// short-lived JWT they then send on callbacks.
func OAuthTokenHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req tokenRequest
		if err := c.BodyParser(&req); err != nil {
			log.WithError(err).Warn("OAuth token request parse failed")
			return c.Status(fiber.StatusBadRequest).JSON(tokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: "Failed to parse token request",
			})
		}

		grantType := strings.TrimSpace(req.GrantType)
		if grantType != "client_credentials" {
			return c.Status(fiber.StatusBadRequest).JSON(tokenErrorResponse{
				Error:            "unsupported_grant_type",
				ErrorDescription: "Only client_credentials is supported",
			})
		}
		clientID := strings.TrimSpace(req.ClientID)
		if clientID == "" || strings.TrimSpace(req.ClientSecret) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(tokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: "client_id and client_secret are required",
			})
		}

		resp, err := oauthissuer.IssueToken(req.ClientID, req.ClientSecret)
		if err != nil {
			switch {
			case errors.Is(err, oauthissuer.ErrNotConfigured):
				log.Warn("OAuth callback issuer not configured; rejecting token request")
				return c.Status(fiber.StatusServiceUnavailable).JSON(tokenErrorResponse{
					Error:            "server_error",
					ErrorDescription: "Token issuer is not configured",
				})
			case errors.Is(err, oauthissuer.ErrInvalidClient):
				log.WithField("client_id", clientID).Warn("OAuth token request: invalid client credentials")
				return c.Status(fiber.StatusUnauthorized).JSON(tokenErrorResponse{
					Error:            "invalid_client",
					ErrorDescription: "Client authentication failed",
				})
			default:
				log.WithError(err).Error("OAuth token issuance failed")
				return c.Status(fiber.StatusInternalServerError).JSON(tokenErrorResponse{
					Error: "server_error",
				})
			}
		}

		log.WithFields(log.Fields{
			"client_id":  clientID,
			"expires_in": resp.ExpiresIn,
		}).Info("OAuth callback token issued")
		return c.Status(fiber.StatusOK).JSON(resp)
	}
}
