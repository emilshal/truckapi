package handlers

import (
	"strconv"
	"truckapi/internal/uifeed"

	"github.com/gofiber/fiber/v2"
)

func UIOrdersFeedHandler(store *uifeed.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		sourceValue := c.Query("source", string(uifeed.SourceCHRobinson))
		source, ok := uifeed.ParseSource(sourceValue)
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid source; expected CHROBINSON or TRUCKSTOP",
			})
		}

		page, _ := strconv.Atoi(c.Query("page", "1"))
		pageSize, _ := strconv.Atoi(c.Query("pageSize", "10"))

		return c.Status(fiber.StatusOK).JSON(store.List(source, page, pageSize))
	}
}
