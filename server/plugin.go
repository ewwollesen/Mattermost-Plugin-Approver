package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

const (
	botUsername = "approvalbot"
	botDisplayName = "Approver"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin
}

// OnActivate is invoked when the plugin is activated.
func (p *Plugin) OnActivate() error {
	// Register the slash command
	err := p.API.RegisterCommand(&model.Command{
		Trigger:          "approver",
		DisplayName:      "Approver Command",
		Description:      "A slash command for approval requests",
		AutoComplete:     true,
		AutoCompleteDesc: "Available commands: new",
		AutoCompleteHint: "[new]",
	})
	
	if err != nil {
		return err
	}
	
	// Try to create the bot account
	botUserID, err := p.ensureBotExists()
	if err != nil {
		p.API.LogWarn("Failed to create bot user", "error", err.Error())
		// Continue with plugin activation even if bot creation fails
	} else if botUserID != "" {
		// Store the bot ID in the plugin's key-value store
		if err := p.API.KVSet("bot_user_id", []byte(botUserID)); err != nil {
			p.API.LogWarn("Failed to store bot user ID", "error", err.Error())
		}
	}
	
	return nil
}

// ensureBotExists makes sure the bot account exists
func (p *Plugin) ensureBotExists() (string, error) {
	// Check if we already have a bot user ID stored
	botUserIDBytes, appErr := p.API.KVGet("bot_user_id")
	if appErr != nil {
		return "", appErr
	}
	
	// If we have a bot user ID, check if the user still exists
	if botUserIDBytes != nil {
		botUserID := string(botUserIDBytes)
		user, err := p.API.GetUser(botUserID)
		if err == nil && user != nil {
			return botUserID, nil
		}
	}
	
	// Try to find an existing user with the bot username
	user, _ := p.API.GetUserByUsername(botUsername)
	if user != nil {
		// User already exists, store its ID and return
		return user.Id, nil
	}
	
	// Create the bot user
	bot := &model.Bot{
		Username:    botUsername,
		DisplayName: botDisplayName,
	}
	
	// Try to create the bot
	createdBot, appErr := p.API.CreateBot(bot)
	if appErr != nil {
		return "", appErr
	}
	
	return createdBot.UserId, nil
}

// ExecuteCommand handles the /approver slash command
func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	// Check if the command is /approver
	if !strings.HasPrefix(args.Command, "/approver") {
		return &model.CommandResponse{
			ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
			Text:         fmt.Sprintf("Unknown command: %s", args.Command),
		}, nil
	}

	// Split the command to get arguments
	splitCmd := strings.Fields(args.Command)
	if len(splitCmd) > 1 && splitCmd[1] == "new" {
		return p.handleNewCommand(args)
	}

	// Default response if no arguments or unrecognized arguments
	return &model.CommandResponse{
		ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
		Text:         "Hello, world! Use '/approver new' to open the approval form.",
	}, nil
}

// handleNewCommand opens a modal for creating a new approval request
func (p *Plugin) handleNewCommand(args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	// Create a new dialog
	dialog := model.Dialog{
		Title:       "New Approval Request",
		CallbackId:  "newApprovalRequest",
		SubmitLabel: "Submit",
		NotifyOnCancel: true,
		Elements: []model.DialogElement{
			{
				DisplayName: "Title",
				Name:        "title",
				Type:        "text",
				SubType:     "text",
				Optional:    false,
			},
			{
				DisplayName: "Description",
				Name:        "description",
				Type:        "textarea",
				Optional:    false,
			},
			{
				DisplayName: "Approver",
				Name:        "approver",
				Type:        "select",
				DataSource:  "users",
				Optional:    false,
			},
		},
	}

	// Show the dialog to the user
	request := model.OpenDialogRequest{
		TriggerId: args.TriggerId,
		URL:       fmt.Sprintf("/plugins/%s/dialog/submit", manifest.Id),
		Dialog:    dialog,
	}

	if err := p.API.OpenInteractiveDialog(request); err != nil {
		return &model.CommandResponse{
			ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
			Text:         "Error opening the dialog: " + err.Error(),
		}, nil
	}

	return &model.CommandResponse{}, nil
}


// sendDirectMessage sends a direct message from the bot to a user
func (p *Plugin) sendDirectMessage(fromUserId, toUserId, title, description string) *model.AppError {
	// Try to get the bot user ID
	botUserIDBytes, appErr := p.API.KVGet("bot_user_id")
	
	// Get information about the requester
	requester, appErr := p.API.GetUser(fromUserId)
	if appErr != nil {
		p.API.LogError("Failed to get requester info", "error", appErr.Error())
		return appErr
	}
	
	// Create the message with formatted content
	message := fmt.Sprintf("**New Approval Request from @%s**\n\n**%s**\n\n%s", 
		requester.Username, title, description)
	
	// First try with the bot user if available
	if len(botUserIDBytes) > 0 {
		botUserID := string(botUserIDBytes)
		p.API.LogDebug("Using bot to send message", "bot_id", botUserID)
		
		// Get the direct channel between the bot and the approver
		botChannel, botErr := p.API.GetDirectChannel(botUserID, toUserId)
		if botErr == nil {
			p.API.LogDebug("Got direct channel for bot", "channel_id", botChannel.Id)
			
			// Create and send the post as the bot
			botPost := &model.Post{
				UserId:    botUserID,
				ChannelId: botChannel.Id,
				Message:   message,
			}
			
			p.API.LogDebug("Sending post as bot", "user_id", botUserID, "channel_id", botChannel.Id, "message", message)
			_, postErr := p.API.CreatePost(botPost)
			if postErr == nil {
				p.API.LogDebug("Successfully sent message as bot")
				return nil
			}
			
			p.API.LogError("Failed to create post as bot, falling back to user", "error", postErr.Error())
		} else {
			p.API.LogError("Failed to get direct channel for bot, falling back to user", "error", botErr.Error())
		}
	}
	
	// Fall back to using the requester's ID if bot is not available or fails
	p.API.LogDebug("Falling back to user for messaging", "from_user_id", fromUserId)
	channel, appErr := p.API.GetDirectChannel(fromUserId, toUserId)
	if appErr != nil {
		p.API.LogError("Failed to get direct channel for user", "error", appErr.Error())
		return appErr
	}
	
	// Create and send the post as the user
	post := &model.Post{
		UserId:    fromUserId,
		ChannelId: channel.Id,
		Message:   message,
	}
	
	p.API.LogDebug("Sending post as user", "user_id", fromUserId, "channel_id", channel.Id, "message", message)
	_, appErr = p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Failed to create post as user", "error", appErr.Error())
	} else {
		p.API.LogDebug("Successfully sent message as user")
	}
	
	return appErr
}

// ServeHTTP handles HTTP requests to the plugin
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	p.API.LogDebug("Received HTTP request", "method", r.Method, "path", path)
	
	if path == "/dialog/submit" {
		p.API.LogDebug("Handling dialog submission")
		p.handleDialogSubmission(w, r)
		return
	}
	
	p.API.LogDebug("Path not found", "path", path)
	http.NotFound(w, r)
}

// handleDialogSubmission processes the submitted dialog
func (p *Plugin) handleDialogSubmission(w http.ResponseWriter, r *http.Request) {
	p.API.LogDebug("Dialog submission received", "method", r.Method, "path", r.URL.Path)
	
	// Log request headers for debugging
	for name, values := range r.Header {
		p.API.LogDebug("Request header", "name", name, "value", strings.Join(values, ", "))
	}
	
	// Read and log the raw request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		p.API.LogError("Failed to read request body", "error", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	// Log the raw request body
	p.API.LogDebug("Raw request body", "body", string(bodyBytes))
	
	// Reset the request body so it can be read again
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	
	request := model.SubmitDialogRequestFromJson(r.Body)
	if request == nil {
		w.WriteHeader(http.StatusBadRequest)
		p.API.LogError("Failed to parse dialog submission request")
		return
	}
	
	p.API.LogDebug("Dialog submission parsed successfully", "callback_id", request.CallbackId)
	
	// Extract form values with safety checks
	var title, description, approverUserId string
	
	if titleVal, ok := request.Submission["title"]; ok {
		if titleStr, ok := titleVal.(string); ok {
			title = titleStr
		} else {
			p.API.LogError("Title is not a string")
			title = "Untitled Request"
		}
	} else {
		p.API.LogError("Title field missing from submission")
		title = "Untitled Request"
	}
	
	if descVal, ok := request.Submission["description"]; ok {
		if descStr, ok := descVal.(string); ok {
			description = descStr
		} else {
			p.API.LogError("Description is not a string")
			description = "No description provided"
		}
	} else {
		p.API.LogError("Description field missing from submission")
		description = "No description provided"
	}
	
	if approverVal, ok := request.Submission["approver"]; ok {
		if approverStr, ok := approverVal.(string); ok {
			approverUserId = approverStr
		} else {
			p.API.LogError("Approver is not a string")
			// Fall back to the requester as the approver
			approverUserId = request.UserId
		}
	} else {
		p.API.LogError("Approver field missing from submission")
		// Fall back to the requester as the approver
		approverUserId = request.UserId
	}
	
	p.API.LogDebug("Processing dialog submission", 
		"title", title,
		"description_length", len(description),
		"approver", approverUserId,
		"requester", request.UserId,
		"channel_id", request.ChannelId)
	
	// Try to get the bot user ID
	botUserIDBytes, appErr := p.API.KVGet("bot_user_id")
	if appErr != nil {
		p.API.LogError("Failed to get bot user ID", "error", appErr.Error())
	} else if len(botUserIDBytes) > 0 {
		p.API.LogDebug("Found bot user ID", "bot_id", string(botUserIDBytes))
	} else {
		p.API.LogDebug("No bot user ID found")
	}
	
	// Log all submission data
	submissionData, _ := json.Marshal(request.Submission)
	p.API.LogDebug("Submission data", "data", string(submissionData))
	
	// Validate that we have all required fields
	if title == "" || description == "" || approverUserId == "" {
		p.API.LogError("Missing required fields", 
			"title_empty", title == "",
			"description_empty", description == "",
			"approver_empty", approverUserId == "")
		
		response := &model.SubmitDialogResponse{
			Error: "Missing required fields. Please fill in all fields and try again.",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}
	
	// Verify the approver exists
	_, appErr = p.API.GetUser(approverUserId)
	if appErr != nil {
		p.API.LogError("Invalid approver user ID", "error", appErr.Error())
		response := &model.SubmitDialogResponse{
			Error: "The selected approver is invalid. Please select a different user.",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}
	
	// Send a direct message to the approver
	err = p.sendDirectMessage(request.UserId, approverUserId, title, description)
	if err != nil {
		p.API.LogError("Failed to send direct message", "error", err.Error())
		response := &model.SubmitDialogResponse{
			Error: "Failed to send message to approver: " + err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		
		// Log the response we're sending back
		responseBytes, _ := json.Marshal(response)
		p.API.LogDebug("Sending error response", "response", string(responseBytes))
		
		json.NewEncoder(w).Encode(response)
		return
	}
	
	// Send confirmation to the user who submitted the request
	p.API.SendEphemeralPost(request.ChannelId, &model.Post{
		UserId:    request.UserId,
		ChannelId: request.ChannelId,
		Message:   "Your approval request has been sent to the approver.",
	})
	
	response := &model.SubmitDialogResponse{}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	// Log the success response
	p.API.LogDebug("Sending success response")
	
	json.NewEncoder(w).Encode(response)
}

func main() {
	plugin.ClientMain(&Plugin{})
}
