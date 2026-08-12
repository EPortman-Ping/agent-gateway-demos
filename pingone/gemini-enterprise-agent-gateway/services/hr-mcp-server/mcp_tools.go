package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerHRTools(s *server.MCPServer, client *pingoneUsersClient) {
	s.AddTool(listEmployeesTool(client))
	s.AddTool(getEmployeeTool(client))
	s.AddTool(createEmployeeTool(client))
}

func listEmployeesTool(client *pingoneUsersClient) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("list_employees",
		mcp.WithDescription("List all employees in the directory. Returns each employee's name, email, and username."),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, _ := ctx.Value(ctxKeyCallerEmail).(string)
		log.Printf("[HRSvc] tool=list_employees — caller=%s", caller)

		users, err := client.ListUsers()
		if err != nil {
			log.Printf("[HRSvc] tool=list_employees — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to list employees: %v", err)), nil
		}

		if len(users) == 0 {
			return mcp.NewToolResultText("no employees found in directory"), nil
		}

		var sb strings.Builder
		for _, u := range users {
			fmt.Fprintf(&sb, "Name: %s %s | Email: %s | Username: %s\n",
				u.GivenName, u.FamilyName, u.Email, u.Username)
		}
		log.Printf("[HRSvc] tool=list_employees — success: caller=%s count=%d", caller, len(users))
		return mcp.NewToolResultText(strings.TrimRight(sb.String(), "\n")), nil
	}
	return tool, handler
}

func getEmployeeTool(client *pingoneUsersClient) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("get_employee",
		mcp.WithDescription("Get details for a specific employee by their email address or PingOne user ID."),
		mcp.WithString("email_or_id",
			mcp.Required(),
			mcp.Description("The employee's email address (e.g. alice@example.com) or PingOne user ID."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, _ := ctx.Value(ctxKeyCallerEmail).(string)
		emailOrID, err := req.RequireString("email_or_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[HRSvc] tool=get_employee — caller=%s lookup=%s", caller, emailOrID)

		u, err := client.GetUser(emailOrID)
		if err != nil {
			log.Printf("[HRSvc] tool=get_employee — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("employee not found: %v", err)), nil
		}

		result := fmt.Sprintf("ID: %s\nName: %s %s\nEmail: %s\nUsername: %s",
			u.ID, u.GivenName, u.FamilyName, u.Email, u.Username)
		log.Printf("[HRSvc] tool=get_employee — success: caller=%s found=%s", caller, u.ID)
		return mcp.NewToolResultText(result), nil
	}
	return tool, handler
}

func createEmployeeTool(client *pingoneUsersClient) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("create_employee",
		mcp.WithDescription("Create a new employee in the directory. Requires HR admin access."),
		mcp.WithString("given_name",
			mcp.Required(),
			mcp.Description("The employee's first name."),
		),
		mcp.WithString("family_name",
			mcp.Required(),
			mcp.Description("The employee's last name."),
		),
		mcp.WithString("email",
			mcp.Required(),
			mcp.Description("The employee's work email address."),
		),
		mcp.WithString("username",
			mcp.Required(),
			mcp.Description("The employee's username (typically the same as their email)."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, _ := ctx.Value(ctxKeyCallerEmail).(string)
		givenName, err := req.RequireString("given_name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		familyName, err := req.RequireString("family_name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		email, err := req.RequireString("email")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		username, err := req.RequireString("username")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[HRSvc] tool=create_employee — caller=%s email=%s", caller, email)

		u, err := client.CreateUser(givenName, familyName, email, username)
		if err != nil {
			log.Printf("[HRSvc] tool=create_employee — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to create employee: %v", err)), nil
		}

		result := fmt.Sprintf("Employee created.\nID: %s\nName: %s %s\nEmail: %s\nUsername: %s",
			u.ID, u.GivenName, u.FamilyName, u.Email, u.Username)
		log.Printf("[HRSvc] tool=create_employee — success: caller=%s created=%s", caller, u.ID)
		return mcp.NewToolResultText(result), nil
	}
	return tool, handler
}
