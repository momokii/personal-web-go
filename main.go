package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html/v2"
	"github.com/momokii/personal-web-go/pkg/medium"
)

func main() {

	engine := html.New("./web/views", ".html")

	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Use(cors.New())
	app.Use(logger.New())
	app.Static("/web", "./web")

	app.Get("/", func(c *fiber.Ctx) error {

		// get medium data
		mediumRes, err := medium.GetMediumProfileData("kelanach")
		if err != nil {
			return c.Render("index", fiber.Map{
				"Medium": []medium.Medium{},
			})
		}

		// get just 6 data
		mediumData := mediumRes.DataMedium[:6]

		return c.Render("index", fiber.Map{
			"Medium": mediumData,
		})
	})

	app.Listen(":3000")

}
