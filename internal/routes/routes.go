package routes

import (
	"truckapi/internal/chrobinson"
	"truckapi/internal/handlers"
	"truckapi/internal/middlewares"
	"truckapi/internal/uifeed"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	log "github.com/sirupsen/logrus"
)

// InitializeRoutes sets up the routes for the Fiber app.
func InitializeRoutes(apiClient *chrobinson.APIClient, feed *uifeed.Store) *fiber.App {
	fiberApp := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if fe, ok := err.(*fiber.Error); ok {
				code = fe.Code
			}
			log.WithError(err).WithFields(log.Fields{
				"method": c.Method(),
				"path":   c.Path(),
				"status": code,
			}).Error("Request failed")

			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		},
	})

	// Middleware
	fiberApp.Use(middlewares.CORS())
	fiberApp.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e interface{}) {
			log.WithField("panic", e).WithFields(log.Fields{
				"method": c.Method(),
				"path":   c.Path(),
			}).Error("Panic recovered")
		},
	}))

	// Webhook endpoint for C.H. Robinson events
	fiberApp.Post("/events/callback/here", handlers.EventCallbackHandler)

	//Post endpoint for our platform to send us driver data
	fiberApp.Post("/driver-data", handlers.HandleDriverData)

	//Post endpoint for available shipment searches
	fiberApp.Post("/v2/shipments/available/searches", handlers.SearchAvailableShipmentsHandler(apiClient))

	//Post endpoint to book a load
	fiberApp.Post("/v1/shipments/books", handlers.BookLoadHandler(apiClient))
	fiberApp.Post("/v1/shipments/mark-booked", handlers.MarkBookedHandler(apiClient))

	//Post endpoint to submit an offer
	// NOTE: auth temporarily disabled per deployment request.
	fiberApp.Post("/v1/shipments/:loadNumber/offers", handlers.SubmitLoadOfferHandler(apiClient))

	// CHRob offer response callback prefers bearer auth, with optional API-key fallback.
	fiberApp.Post("/offerResponse/callback/here", middlewares.OfferCallbackAuthMiddleware(), handlers.OfferResponseHandler)

	//Post endpoint for receiving all of our booked shipment details
	fiberApp.Post("/shipmentDetails/callback/here", handlers.ShipmentDetailsHandler)

	// Route for combined shipment info
	fiberApp.Get("/v1/shipments/combined", handlers.CombinedShipmentInfoHandler(apiClient))

	// Prototype feed: show raw mapped orders in UI.
	fiberApp.Get("/api/orders", handlers.UIOrdersFeedHandler(feed))

	// Debug mock Loader receiver for duplicate testing.
	// Point `LOADER_ORDERS_BASE_URL` to `http://127.0.0.1:<port>/debug/mock-loader`.
	fiberApp.Post("/debug/mock-loader/api/v1/loader/orders", handlers.MockLoaderCreateOrderHandler())
	fiberApp.Get("/debug/mock-loader/summary", handlers.MockLoaderSummaryHandler())
	fiberApp.Get("/debug/mock-loader/orders", handlers.MockLoaderListHandler())
	fiberApp.Post("/debug/mock-loader/reset", handlers.MockLoaderResetHandler())

	// Health check endpoint
	fiberApp.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "OK"})
	})

	fiberApp.Get("/v1/offers", handlers.FetchAllOffersHandler)

	return fiberApp
}
