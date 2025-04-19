package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/momokii/go-llmbridge/pkg/openai"
	"github.com/momokii/personal-web-go/pkg/medium"
	"github.com/momokii/personal-web-go/pkg/utils"
)

type BotMessagesReq struct {
	Messages []openai.OAMessageReq `json:"messages"`
}

type dashboardHandler struct {
	openaiClient openai.OpenAI
}

func NewDashboardHandler(
	openaiClient openai.OpenAI,
) *dashboardHandler {
	return &dashboardHandler{
		openaiClient: openaiClient,
	}
}

func (h *dashboardHandler) DashboardView(c *fiber.Ctx) error {
	mediumData := []medium.Medium{}

	// get medium data
	mediumRes, err := medium.GetMediumProfileData("kelanach")
	if err != nil {
		return c.Render("modern", fiber.Map{
			"Medium": []medium.Medium{},
		})
	}

	if len(mediumRes.DataMedium) != 0 {
		// get just 6 data
		mediumData = mediumRes.DataMedium[:6]
	}

	return c.Render("modern", fiber.Map{
		"Medium": mediumData,
	})
}

func (h *dashboardHandler) ChatBotMessages(c *fiber.Ctx) error {

	// get all messages request from user that include the newest question
	messages_req := new(BotMessagesReq)
	if err := c.BodyParser(messages_req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "failed to parse request body",
		})
	}

	// combine the messages with the system message
	messages := []openai.OAMessageReq{
		{
			Role:    "system",
			Content: utils.BASE_KNOWLEDGE,
		},
	}

	messages = append(messages, messages_req.Messages...)

	bot_response, err := h.openaiClient.OpenAIGetFirstContentDataResp(&messages, false, nil, false, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "failed to get response from OpenAI",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"error":   false,
		"message": "success process messages",
		"data": fiber.Map{
			"content": bot_response.Content,
		},
	})
}
