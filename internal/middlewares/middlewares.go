package middlewares

import (
	"log"
	"os"

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

// APIKeyMiddleware returns a middleware handler function for API key validation
func APIKeyMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Retrieve the API key from the request header
		apiKey := c.Get("X-API-KEY")

		// Check if the API key is missing
		if apiKey == "" {
			// Log the missing API key
			log.Println("API key missing")

			// Return a 401 Unauthorized status with an error message
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "API key missing",
			})
		}

		// Get the expected API key from environment variables
		expectedAPIKey := os.Getenv("API_KEY")

		// Check if the provided API key matches the expected API key
		if apiKey != expectedAPIKey {
			// Log the invalid API key
			log.Println("Invalid API key")

			// Return a 401 Unauthorized status with an error message
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid API key",
			})
		}

		// If the API key is valid, proceed to the next handler
		return c.Next()
	}
}
