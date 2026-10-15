package main

import (
	"context"
	"net/http"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CourseIn struct {
	CourseID string `json:"course_id" jsonschema:"Canvas course ID (get it from list_courses)"`
}
type CoursesOut struct {
	Courses []Course `json:"courses"`
}
type AssignmentsOut struct {
	Assignments []Assignment `json:"assignments"`
}
type AnnouncementsOut struct {
	Announcements []Announcement `json:"announcements"`
}
type DownloadOut struct {
	Dir string `json:"dir"`
}

func runMCP(client *http.Client, cfg Config) (error) {

	s := mcp.NewServer(&mcp.Implementation{Name: "mycanvas", Version: "v0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{Name: "list_courses", Description: "List the user's Canvas courses with their IDs"},
	func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, CoursesOut, error) {
		c, err := getCoursesForUser(client, cfg.Token)
		return nil, CoursesOut{c}, err
	})

	mcp.AddTool(s, &mcp.Tool{Name: "list_assignments", Description: "List assignments for a course"},
	func(ctx context.Context, _ *mcp.CallToolRequest, in CourseIn) (*mcp.CallToolResult, AssignmentsOut, error) {
		a, err := getAssignmentsForCourse(client, in.CourseID, cfg.Token)
		return nil, AssignmentsOut{a}, err
	})

	mcp.AddTool(s, &mcp.Tool{Name: "list_announcements", Description: "List announcements for a course"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in CourseIn) (*mcp.CallToolResult, AnnouncementsOut, error) {
			a, err := getAnnouncementsForCourse(client, in.CourseID, cfg.Token)
			return nil, AnnouncementsOut{a}, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "download_course_files", Description: "Download all module files for a course into the configured folder"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in CourseIn) (*mcp.CallToolResult, DownloadOut, error) {
			err := getAllFilesForCourse(client, in.CourseID, cfg.Dir, cfg.Token)
			return nil, DownloadOut{Dir: cfg.Dir}, err
		})

return s.Run(context.Background(), &mcp.StdioTransport{})
}

