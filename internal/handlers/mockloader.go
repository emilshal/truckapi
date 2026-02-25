package handlers

import (
	"strconv"
	"truckapi/internal/loader"
	"truckapi/internal/mockloader"

	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"
)

func MockLoaderCreateOrderHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var order loader.LoaderOrder
		if err := c.BodyParser(&order); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid loader order payload",
			})
		}

		received := mockloader.DefaultStore.Add(order)
		log.WithFields(log.Fields{
			"orderNumber":   order.OrderNumber,
			"source":        order.Source,
			"duplicate":     received.Duplicate,
			"count_for_key": received.CountForKey,
		}).Info("Mock Loader received order")

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"ok":          true,
			"duplicate":   received.Duplicate,
			"countForKey": received.CountForKey,
			"orderNumber": order.OrderNumber,
		})
	}
}

func MockLoaderSummaryHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		top, _ := strconv.Atoi(c.Query("top", "20"))
		return c.Status(fiber.StatusOK).JSON(mockloader.DefaultStore.Summary(top))
	}
}

func MockLoaderListHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		page, _ := strconv.Atoi(c.Query("page", "1"))
		pageSize, _ := strconv.Atoi(c.Query("pageSize", "100"))
		return c.Status(fiber.StatusOK).JSON(mockloader.DefaultStore.List(page, pageSize))
	}
}

func MockLoaderResetHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		mockloader.DefaultStore.Reset()
		log.Info("Mock Loader store reset")
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"ok": true})
	}
}
