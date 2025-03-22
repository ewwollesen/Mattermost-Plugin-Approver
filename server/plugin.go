package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

const (
	botUsername = "approvalbot"
	botDisplayName = "ApprovalBot"
	botDescription = "A bot for the Approver plugin"
	botEmail = "approvalbot@example.com"
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
	
	// Create or get the bot user
	botUserID, appErr := p.ensureBotUser()
	if appErr != nil {
		return appErr
	}
	
	// Store the bot user ID in the KV store
	if err := p.API.KVSet("bot_user_id", []byte(botUserID)); err != nil {
		return fmt.Errorf("failed to store bot user ID: %w", err)
	}
	
	return nil
}

// ensureBotUser creates a bot user if it doesn't exist
func (p *Plugin) ensureBotUser() (string, *model.AppError) {
	// Check if we already have a bot user ID stored
	botUserIDBytes, err := p.API.KVGet("bot_user_id")
	if err != nil {
		return "", err
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
	existingBot, appErr := p.API.GetUserByUsername(botUsername)
	if appErr == nil && existingBot != nil {
		// User already exists, store its ID and return
		return existingBot.Id, nil
	}
	
	// Get the first team
	teams, err := p.API.GetTeams()
	if err != nil {
		return "", err
	}
	
	if len(teams) == 0 {
		return "", &model.AppError{Message: "No teams found"}
	}
	
	// Generate a unique username to avoid conflicts
	uniqueUsername := botUsername + "_" + model.NewId()[:5]
	
	// Create the bot user with the unique username
	bot := &model.Bot{
		Username:    uniqueUsername,
		DisplayName: botDisplayName,
		Description: botDescription,
	}
	
	createdBot, err := p.API.CreateBot(bot)
	if err != nil {
		return "", err
	}
	
	// No need to add a bot to a team
	
	return createdBot.Id, nil
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
				Type:        "text",
				SubType:     "textarea",
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

// handleSubmitDialog processes the submitted dialog
func (p *Plugin) handleSubmitDialog(request *model.SubmitDialogRequest) (*model.SubmitDialogResponse, *model.AppError) {
	// Extract form values
	title := request.Submission["title"].(string)
	description := request.Submission["description"].(string)
	approverUserId := request.Submission["approver"].(string)
	
	// Send a direct message to the approver
	err := p.sendDirectMessage(request.UserId, approverUserId, title, description)
	if err != nil {
		return &model.SubmitDialogResponse{
			Error: "Failed to send message to approver: " + err.Error(),
		}, nil
	}
	
	// Send confirmation to the user who submitted the request
	p.API.SendEphemeralPost(request.ChannelId, &model.Post{
		UserId:    request.UserId,
		ChannelId: request.ChannelId,
		Message:   "Your approval request has been sent to the approver.",
	})
	
	return &model.SubmitDialogResponse{}, nil
}

// sendDirectMessage sends a direct message from the bot to a user
func (p *Plugin) sendDirectMessage(fromUserId, toUserId, title, description string) *model.AppError {
	// Get the bot user ID
	botUserIDBytes, appErr := p.API.KVGet("bot_user_id")
	if appErr != nil {
		return appErr
	}
	
	if botUserIDBytes == nil {
		return &model.AppError{Message: "Bot user ID not found"}
	}
	
	botUserID := string(botUserIDBytes)
	
	// Get the direct channel between the bot and the user
	channel, appErr := p.API.GetDirectChannel(botUserID, toUserId)
	if appErr != nil {
		return appErr
	}
	
	// Get information about the requester
	requester, appErr := p.API.GetUser(fromUserId)
	if appErr != nil {
		return appErr
	}
	
	// Create the message with formatted content
	message := fmt.Sprintf("**New Approval Request from @%s**\n\n**%s**\n\n%s", 
		requester.Username, title, description)
	
	// Create and send the post as the bot
	post := &model.Post{
		UserId:    botUserID,
		ChannelId: channel.Id,
		Message:   message,
		Props: model.StringInterface{
			"from_bot": "true",
		},
	}
	
	_, appErr = p.API.CreatePost(post)
	return appErr
}

// ServeHTTP handles HTTP requests to the plugin
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	if path == "/dialog/submit" {
		p.handleDialogSubmission(w, r)
		return
	}
	
	http.NotFound(w, r)
}

// handleDialogSubmission processes the submitted dialog
func (p *Plugin) handleDialogSubmission(w http.ResponseWriter, r *http.Request) {
	request := model.SubmitDialogRequestFromJson(r.Body)
	if request == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	// Extract form values
	title := request.Submission["title"].(string)
	description := request.Submission["description"].(string)
	approverUserId := request.Submission["approver"].(string)
	
	// Send a direct message to the approver
	err := p.sendDirectMessage(request.UserId, approverUserId, title, description)
	if err != nil {
		response := &model.SubmitDialogResponse{
			Error: "Failed to send message to approver: " + err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
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
	json.NewEncoder(w).Encode(response)
}

func main() {
	plugin.ClientMain(&Plugin{})
}
