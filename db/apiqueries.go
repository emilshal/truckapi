package db

import (
	"context"
	"fmt"
	"os"
	"regexp"

	openai "github.com/sashabaranov/go-openai"
	log "github.com/sirupsen/logrus"
)

var openaiClient *openai.Client

func init() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Warn("OpenAI API key not found in environment variables; OpenAI features disabled")
		return
	}

	openaiClient = openai.NewClient(apiKey)
	log.Info("OpenAI client initialized successfully")
}

func getCityDetails(cityName, nearbyCityName string) (string, string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Error("OpenAI API key not found in environment variables")
		return "", "", fmt.Errorf("OpenAI API key not found in environment variables")
	}

	client := openai.NewClient(apiKey)
	prompt := fmt.Sprintf("Please provide the state and zip code for the city of %s, which is within a 250 mile radius of %s, in the following format: 'State: <state>, Zip: <zip>'.", cityName, nearbyCityName)

	log.Infof("Sending prompt to OpenAI: %s", prompt)
	response, err := client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
		Model: "gpt-3.5-turbo",
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    "system",
				Content: "You are a helpful assistant.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens: 50,
	})
	if err != nil {
		log.Errorf("Error creating chat completion: %v", err)
		return "", "", err
	}

	log.Infof("OpenAI response: %+v", response)
	if len(response.Choices) == 0 {
		log.Error("No choices returned in OpenAI response")
		return "", "", fmt.Errorf("no choices returned in OpenAI response")
	}

	state, zip := parseOpenAIResponse(response.Choices[0].Message.Content)
	log.Infof("Parsed response - State: %s, Zip: %s", state, zip)

	return state, zip, nil
}

func parseOpenAIResponse(response string) (string, string) {
	// Regular expressions to capture state and zip code
	re1 := regexp.MustCompile(`State: (?P<state>[A-Za-z\s]+), Zip: (?P<zip>[0-9]+)`)
	re2 := regexp.MustCompile(`is located in (?P<state>[A-Za-z\s]+)\. Zip code for .+: (?P<zip>[0-9]+)\.`)

	match := re1.FindStringSubmatch(response)
	if match == nil {
		match = re2.FindStringSubmatch(response)
	}

	if match == nil {
		log.Warnf("Response parsing failed, could not find state and zip: %s", response)
		return "", ""
	}

	result := make(map[string]string)
	for i, name := range re1.SubexpNames() {
		if i != 0 && name != "" && i < len(match) {
			result[name] = match[i]
		}
	}

	state := result["state"]
	zip := result["zip"]

	log.Infof("Parsing response: %s -> State: %s, Zip: %s", response, state, zip)
	return state, zip
}
