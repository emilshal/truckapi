package middlewares

import (
	"crypto/subtle"
	"log"
	"strings"
	"truckapi/pkg/config"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

// CORS Headers to access different domains
func CORS() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
		AllowMethods: "GET, POST, OPTIONS, PUT, DELETE",
	})

}

func envTruthy(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(config.GetEnv(key, "")))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func apiKeyValid(c *fiber.Ctx) bool {
	apiKey := c.Get("X-API-KEY")
	if apiKey == "" {
		return false
	}
	expectedAPIKey := strings.TrimSpace(config.GetEnv(config.APIKey, ""))
	if expectedAPIKey == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedAPIKey)) == 1
}

func bearerToken(c *fiber.Ctx) string {
	authz := strings.TrimSpace(c.Get("Authorization"))
	if authz == "" {
		return ""
	}
	parts := strings.SplitN(authz, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// APIKeyMiddleware returns a middleware handler function for API key validation
func APIKeyMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if apiKeyValid(c) {
			return c.Next()
		}
		if c.Get("X-API-KEY") == "" {
			log.Println("API key missing")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "API key missing"})
		}
		log.Println("Invalid API key")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid API key"})
	}
}

// BidEndpointAuthMiddleware protects internal-facing bid submission routes.
func BidEndpointAuthMiddleware() fiber.Handler {
	return APIKeyMiddleware()
}

// OfferCallbackAuthMiddleware accepts a CHRob callback bearer token (preferred)
// and can optionally allow API key fallback for backward compatibility.
func OfferCallbackAuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		expectedBearer := strings.TrimSpace(config.GetEnv(config.CHRobCallbackBearerToken, ""))
		allowAPIKey := envTruthy(config.CHRobCallbackAllowAPIKey, true)

		if expectedBearer != "" {
			got := bearerToken(c)
			if got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(expectedBearer)) == 1 {
				return c.Next()
			}
			if !allowAPIKey {
				log.Println("Invalid or missing callback bearer token")
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "Invalid or missing bearer token",
				})
			}
		}

		if apiKeyValid(c) {
			return c.Next()
		}

		if expectedBearer != "" {
			log.Println("Callback auth failed (bearer/api key)")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Authentication failed",
			})
		}

		if allowAPIKey {
			if strings.TrimSpace(c.Get("X-API-KEY")) == "" {
				log.Println("API key missing")
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "API key missing"})
			}
			log.Println("Invalid API key")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid API key"})
		}

		log.Println("Offer callback auth misconfigured: no bearer token and API key fallback disabled")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Offer callback auth is misconfigured",
		})
	}
}
