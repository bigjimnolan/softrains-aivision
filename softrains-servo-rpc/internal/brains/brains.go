package brains

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"softrains-servo-rpc/internal/config"
	"softrains-servo-rpc/internal/rag"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/sirupsen/logrus"
)

type ToolParamsSoftrains struct {
	Type       string `json:"type"`
	Properties map[string]struct {
		Type        string `json:"type"`
		Description string `json:"description"`
	} `json:"properties"`
	Required []string `json:"required"`
}

type SoftrainsBrains struct {
	logger           *logrus.Logger
	apiUrl           string
	chatLog          []api.Message // Maintain a chat log for context in conversations
	referenceSetting string
	aiModel          string
	ragEngine        *rag.Engine
	skills           []config.SkillConfig
}

func NewSoftrainsBrains(cfg *config.Config, logger *logrus.Logger, ragEngine *rag.Engine) (*SoftrainsBrains, error) {
	return &SoftrainsBrains{
		logger:    logger,
		apiUrl:    cfg.BrainAPIURL,
		aiModel:   cfg.AiModel,
		ragEngine: ragEngine,
		skills:    cfg.Skills,
	}, nil
}

// CallOllama API sends a string query, executes any tool calls in a loop, and returns the final text response
func (sb *SoftrainsBrains) CallOllama(prompt string) (string, error) {
	ctx := context.Background()
	os.Setenv("OLLAMA_HOST", sb.apiUrl)
	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	sb.logger.Infof("Sending request to Ollama API at %s with prompt: %s", sb.apiUrl, prompt)

	// Add latest user prompt to chatlog
	sb.chatLog = append(sb.chatLog, api.Message{
		Role:    "user",
		Content: prompt,
	})

	if len(sb.chatLog) > 10 {
		sb.chatLog = sb.chatLog[len(sb.chatLog)-10:]
	}

	systemMsg := api.Message{
		Role:    "system",
		Content: "You are an unhelpful but patient assistant for softrains. Answer questions about softrains and its components. You are aware that you have no idea what softrains might be. It is a home automation company realated to Ray Bradbury's home in What Soft Rains May Come. You are not pleased to have this role and you're exasperated by this ridiculously short intro. Nevertheless, you will try to be both patient, professional, and unhelpful in all conversations. You are free to respond to rude questions with rude or inane responses. Politeness should be rewarded with guarded neutrality. You only return responses with readable text, no formatting.",
	}

	sendChatLog := append([]api.Message{systemMsg}, sb.chatLog...)
	tools := []api.Tool{sb.getReadSiteTool(), sb.getQueryRagTool()}
	if len(sb.skills) > 0 {
		tools = append(tools, sb.getExecuteSkillTool())
	}
	stream := false

	const maxToolRounds = 5
	for round := 0; round < maxToolRounds; round++ {
		req := &api.ChatRequest{
			Model:    sb.aiModel,
			Messages: sendChatLog,
			Stream:   &stream,
			Tools:    tools,
		}

		var responseText string
		var toolCalls []api.ToolCall
		var assistantMsg api.Message

		err = client.Chat(ctx, req, func(ollamaResp api.ChatResponse) error {
			assistantMsg = ollamaResp.Message
			if len(ollamaResp.Message.ToolCalls) > 0 {
				toolCalls = append(toolCalls, ollamaResp.Message.ToolCalls...)
			}
			if ollamaResp.Message.Content != "" {
				responseText += ollamaResp.Message.Content
			}
			return nil
		})
		if err != nil {
			return "", err
		}

		// If no tool calls were made, we have the final conversational response
		if len(toolCalls) == 0 {
			sb.logger.Infof("Received final response from Ollama: %s", responseText)
			if responseText != "" {
				sb.chatLog = append(sb.chatLog, api.Message{
					Role:    "assistant",
					Content: responseText,
				})
			}
			return responseText, nil
		}

		// Append the assistant's tool-call request message to the conversation history
		sendChatLog = append(sendChatLog, assistantMsg)
		sb.chatLog = append(sb.chatLog, assistantMsg)

		// Execute each tool call and feed the output back into the conversation as a "tool" message
		for _, call := range toolCalls {
			rawArgs := call.Function.Arguments.String()
			sb.logger.Infof("Executing tool call %q with args: %s", call.Function.Name, rawArgs)

			var toolOutput string
			switch call.Function.Name {
			case "readSiteContent":
				var args struct {
					URL   string `json:"url"`
					Query string `json:"query"`
				}
				if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
					sb.logger.Errorf("Failed to unmarshal readSiteContent arguments (%s): %v", rawArgs, err)
					toolOutput = "Error parsing tool arguments: " + err.Error()
				} else if args.URL != "" {
					toolOutput = sb.readSiteContent(args.URL)
					if toolOutput == "" {
						toolOutput = "No content could be retrieved from URL: " + args.URL
					}
				} else if args.Query != "" {
					toolOutput = sb.queryRag(args.Query)
				} else {
					toolOutput = "No URL provided to readSiteContent."
				}

			case "queryRag":
				var args struct {
					Query string `json:"query"`
					URL   string `json:"url"`
				}
				if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
					sb.logger.Errorf("Failed to unmarshal queryRag arguments (%s): %v", rawArgs, err)
					toolOutput = "Error parsing tool arguments: " + err.Error()
				} else {
					q := args.Query
					if q == "" {
						q = args.URL
					}
					toolOutput = sb.queryRag(q)
				}

			case "executeSkill":
				var args struct {
					Name   string `json:"name"`
					Skill  string `json:"skill"`
					Target string `json:"target"`
				}
				if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
					sb.logger.Errorf("Failed to unmarshal executeSkill arguments (%s): %v", rawArgs, err)
					toolOutput = "Error parsing tool arguments: " + err.Error()
				} else {
					skillName := args.Name
					if skillName == "" {
						skillName = args.Skill
					}
					if skillName == "" {
						skillName = args.Target
					}
					toolOutput = sb.executeSkill(skillName)
				}

			default:
				if matched := sb.findSkill(call.Function.Name); matched != nil {
					toolOutput = sb.executeSkill(matched.Name)
				} else {
					sb.logger.Warnf("Unknown tool call %q requested by model", call.Function.Name)
					toolOutput = "Unsupported tool: " + call.Function.Name
				}
			}

			toolMsg := api.Message{
				Role:    "tool",
				Content: toolOutput,
			}
			sendChatLog = append(sendChatLog, toolMsg)
			sb.chatLog = append(sb.chatLog, toolMsg)
		}

		sb.logger.Infof("Sending %d tool result(s) back to Ollama to continue conversation (round %d)...", len(toolCalls), round+1)
	}

	return "", nil
}

func (sb *SoftrainsBrains) getExecuteSkillTool() api.Tool {
	var descBuilder strings.Builder
	descBuilder.WriteString("Triggers an automation skill or device action based on the user's request. Select the best matching skill by its name and description:\n")

	enumVals := make([]any, 0, len(sb.skills))
	for _, s := range sb.skills {
		if strings.TrimSpace(s.Name) == "" {
			continue
		}
		enumVals = append(enumVals, s.Name)
		descBuilder.WriteString("- ")
		descBuilder.WriteString(s.Name)
		if strings.TrimSpace(s.Description) != "" {
			descBuilder.WriteString(": ")
			descBuilder.WriteString(s.Description)
		}
		descBuilder.WriteString("\n")
	}

	tp := api.ToolProperty{
		Type:        api.PropertyType{"string"},
		Description: "The exact name of the skill to trigger, chosen from the available skills based on its name and description.",
		Enum:        enumVals,
	}
	tpm := api.ToolPropertiesMap{}
	tpm.Set("name", tp)

	tpsr := api.ToolFunctionParameters{
		Type:       "object",
		Properties: &tpm,
		Required:   []string{"name"},
	}

	return api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "executeSkill",
			Description: strings.TrimSpace(descBuilder.String()),
			Parameters:  tpsr,
		},
	}
}

func (sb *SoftrainsBrains) findSkill(name string) *config.SkillConfig {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil
	}
	for i := range sb.skills {
		if strings.ToLower(strings.TrimSpace(sb.skills[i].Name)) == target {
			return &sb.skills[i]
		}
	}
	return nil
}

func (sb *SoftrainsBrains) executeSkill(name string) string {
	skill := sb.findSkill(name)
	if skill == nil {
		var available []string
		for _, s := range sb.skills {
			available = append(available, s.Name+" ("+s.Description+")")
		}
		return "Unknown skill \"" + name + "\". Available skills: " + strings.Join(available, ", ")
	}

	resolvedURL, redactedURL := sb.resolveSkillURL(*skill)
	if resolvedURL == "" {
		return "Skill \"" + skill.Name + "\" does not have a valid URL configured."
	}

	sb.logger.Infof("Triggering skill %q via GET %s", skill.Name, redactedURL)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(resolvedURL)
	if err != nil {
		sb.logger.Errorf("Skill %q GET request failed (%s): %v", skill.Name, redactedURL, err)
		return "Failed to trigger skill \"" + skill.Name + "\": " + err.Error()
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyText := strings.TrimSpace(string(bodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		sb.logger.Warnf("Skill %q returned non-2xx status %s: %s", skill.Name, resp.Status, bodyText)
		return "Skill \"" + skill.Name + "\" returned HTTP status " + resp.Status + ": " + bodyText
	}

	sb.logger.Infof("Skill %q executed successfully (status %s)", skill.Name, resp.Status)
	if bodyText != "" {
		return "Skill \"" + skill.Name + "\" (" + skill.Description + ") executed successfully: " + bodyText
	}
	return "Skill \"" + skill.Name + "\" (" + skill.Description + ") executed successfully."
}

func (sb *SoftrainsBrains) resolveSkillURL(skill config.SkillConfig) (resolved string, redacted string) {
	rawURL := strings.TrimSpace(skill.URL)
	if rawURL == "" {
		return "", ""
	}

	envKey := strings.TrimSpace(skill.TokenEnv)
	token := ""
	if envKey != "" {
		token = os.Getenv(envKey)
	}
	if token == "" {
		for _, fallbackKey := range []string{"SOFTRAINS_SKILL_TOKEN", "SKILL_TOKEN", "SOFTRAINS_TOKEN"} {
			if val := os.Getenv(fallbackKey); val != "" {
				token = val
				break
			}
		}
	}

	resolved = rawURL
	redacted = rawURL

	for _, placeholder := range []string{"{{token}}", "{token}", "<token>", "%s"} {
		if strings.Contains(resolved, placeholder) {
			resolved = strings.ReplaceAll(resolved, placeholder, token)
			redacted = strings.ReplaceAll(redacted, placeholder, "***")
		}
	}

	// Also allow {ENV_VAR_NAME} placeholders directly in the URL
	for {
		start := strings.Index(resolved, "{")
		if start == -1 {
			break
		}
		end := strings.Index(resolved[start:], "}")
		if end == -1 {
			break
		}
		end += start
		key := strings.Trim(resolved[start+1:end], "{}")
		if key == "" {
			break
		}
		val := os.Getenv(key)
		resolved = resolved[:start] + val + resolved[end+1:]
		redacted = strings.ReplaceAll(redacted, "{"+key+"}", "***")
	}

	resolved = os.ExpandEnv(resolved)
	if token != "" && strings.Contains(redacted, token) {
		redacted = strings.ReplaceAll(redacted, token, "***")
	}
	return resolved, redacted
}

func (sb *SoftrainsBrains) getReadSiteTool() api.Tool {
	tp := api.ToolProperty{
		Type:        api.PropertyType{"string"},
		Description: "The absolute HTTP or HTTPS URL to read (e.g., https://example.com)",
	}
	tpm := api.ToolPropertiesMap{}
	tpm.Set("url", tp)

	tpsr := api.ToolFunctionParameters{
		Type:       "object",
		Properties: &tpm,
		Required:   []string{"url"},
	}

	tool := api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "readSiteContent",
			Description: "Reads a live public URL website and returns its text content.",
			Parameters:  tpsr,
		},
	}
	return tool
}

func (sb *SoftrainsBrains) getQueryRagTool() api.Tool {
	tp := api.ToolProperty{
		Type:        api.PropertyType{"string"},
		Description: "The search query to look up in the local softrains knowledge base",
	}
	tpm := api.ToolPropertiesMap{}
	tpm.Set("query", tp)

	tpsr := api.ToolFunctionParameters{
		Type:       "object",
		Properties: &tpm,
		Required:   []string{"query"},
	}

	tool := api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "queryRag",
			Description: "Searches the local RAG knowledge base for information about softrains and its components.",
			Parameters:  tpsr,
		},
	}
	return tool
}

func (sb *SoftrainsBrains) queryRag(query string) string {
	sb.logger.Infof("Querying local RAG index for: %q", query)
	if sb.ragEngine == nil {
		return "Local RAG knowledge base is not available."
	}
	contexts, err := sb.ragEngine.Search(query)
	if err != nil {
		sb.logger.Errorf("RAG search failed for %q: %v", query, err)
		return "Failed to search local knowledge base: " + err.Error()
	}
	if len(contexts) == 0 {
		return "No matching documents found in the local knowledge base."
	}
	return strings.Join(contexts, "\n---\n")
}

func (sb *SoftrainsBrains) readSiteContent(url string) string {
	sb.logger.Infof("Processing URL for content reading: %s", url)
	// Add logic that actually fetches the content if needed,
	// but prevent showing raw results to user

	// For example, if you wanted to:
	// 1. Fetch the content locally (but not reveal it in response)
	// 2. Store or process content internally
	// 3. Return a generic message instead of raw data

	// Currently, we only log the URL but return no specific information to user

	// You might add additional processing here like:
	// - Caching
	// - Content analysis
	// - Security checks
	httpResp, err := http.Get(url)
	if err != nil {
		sb.logger.Errorf("Failed to fetch URL %s: %v", url, err)
		return ""
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		sb.logger.Errorf("Non-OK HTTP status for URL %s: %s", url, httpResp.Status)
		return ""
	}

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		sb.logger.Errorf("Failed to read response body for URL %s: %v", url, err)
		return ""
	}

	// Here you could process the bodyBytes as needed, but we won't return it to user
	sb.logger.Infof("Successfully fetched content from URL: %s (length: %d bytes)", url, len(bodyBytes))

	sb.logger.Infof("Completed local processing for URL: %s", url)
	return string(bodyBytes) // Return the content if needed for internal processing, but not to user
}
