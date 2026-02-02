package routes

import (
	"truckapi/internal/chrobinson"
	"truckapi/internal/handlers"
	"truckapi/internal/middlewares"

	"github.com/gofiber/fiber/v2"
)

// InitializeRoutes sets up the routes for the Fiber app.
func InitializeRoutes(apiClient *chrobinson.APIClient) *fiber.App {
	fiberApp := fiber.New()

	// Middleware
	fiberApp.Use(middlewares.CORS())

	// Webhook endpoint for C.H. Robinson events
	fiberApp.Post("/events/callback/here", handlers.EventCallbackHandler)

	//Post endpoint for our platform to send us driver data
	fiberApp.Post("/driver-data", handlers.HandleDriverData)

	//Post endpoint for available shipment searches
	fiberApp.Post("/v2/shipments/available/searches", handlers.SearchAvailableShipmentsHandler(apiClient))

	//Post endpoint to book a load
	fiberApp.Post("/v1/shipments/books", handlers.BookLoadHandler(apiClient))

	//Post endpoint to submit an offer
	fiberApp.Post("/v1/shipments/:loadNumber/offers", handlers.SubmitLoadOfferHandler(apiClient))

	// Apply API key middleware to the specific route
	fiberApp.Post("/offerResponse/callback/here", middlewares.APIKeyMiddleware(), handlers.OfferResponseHandler)

	//Post endpoint for receiving all of our booked shipment details
	fiberApp.Post("/shipmentDetails/callback/here", handlers.ShipmentDetailsHandler)

	// Route for combined shipment info
	fiberApp.Get("/v1/shipments/combined", handlers.CombinedShipmentInfoHandler(apiClient))

	// Health check endpoint
	fiberApp.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "OK"})
	})

	fiberApp.Get("/v1/offers", handlers.FetchAllOffersHandler)

	return fiberApp
}
