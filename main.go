package main

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"io/fs"
	"errors"
	"flag"
	"net/http"
	"sync"
	"strconv"
	"os"
	"io"
	"time"
	"encoding/json"
	"path/filepath"
)

type Module struct {
	Id int `json:"id"`
	ItemsUrl string `json:"items_url"`
}

type ModuleItem struct {
	Type string `json:"type"`
	Url string `json:"url"`
}

type FileItem struct {
	Url string `json:"url"`
}

type FileObj struct {
	Url string `json:"url"`
	LockedForUser bool `json:"locked_for_user"`
	Title string `json:"display_name"`
}

type Config struct {

	Dir string `toml:"dest"`
	Token string `toml:"token"`
}

type Submission struct {
	Score *float64 `json:"score"`
	Grade *string `json:"grade"`
	WorkflowState string `json:"workflow_state"`
	GradedAt *string `json:"graded_at"`
}

type Announcement struct {
	Title string `json:"title"`
	Message string `json:"message"`
	PostedAt string `json:"posted_at"`
}

type Assignment struct {
	Name string `json:"name"`
	Description string `json:"description"`
	DueDate string `json:"due_at"`
	CourseId int `json:"course_id"`
	PointsPossible float64 `json:"points_possible"`
	Submitted bool `json:"has_submitted_submissions"`
	Submission *Submission `json:"submission,omitempty"`
}

type Course struct {

	Name string `json:"name"`
	CourseId int `json:"id"`
	StartAt string `json:"start_at"`
	EndAt string `json:"end_at"`
}


func main() {

	// check proper usage
	if len(os.Args) < 2 {
		fmt.Println("usage is wrong")
		os.Exit(1)
	}

	var config Config;
	err := getConfig(&config)
	if err != nil {
		fmt.Print("Can't read config: ", err)
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
		},
	}
	switch os.Args[1] {

		case "mcp":
			fs := flag.NewFlagSet("mcp", flag.ExitOnError)
			token := fs.String("token", config.Token, "Canvas API token")
			dest := fs.String("dest", config.Dir, "download directory")
			fs.Parse(os.Args[2:])
			config.Token, config.Dir = *token, *dest

			if config.Token == "" {
				fmt.Fprintln(os.Stderr, "mcp: no API token (set API_TOKEN, config file, or --token)")
				os.Exit(1)
			}
			if err := runMCP(client, config); err != nil {
				fmt.Fprintln(os.Stderr, "mcp:", err)
				os.Exit(1)
			}

		case "directory": // list directory
			if config.Dir == "" {
				fmt.Println("No destination dir in config")
				os.Exit(1)
			}
			fmt.Println("Destination directory is " +config.Dir)

		case "files": // get files for course


			fs := flag.NewFlagSet("assignments", flag.ExitOnError)
			token := fs.String("token", config.Token, "Canvas API token")
			course := fs.String("courseid", "", "Course id number")
			dest := fs.String("dest",config.Dir, "directory")
			fs.Parse(os.Args[2:])
			
			if *dest == "" || *course == "" || *token == "" {
				fmt.Printf(" courseid, destination folder and token need to be specified either on config file or as cli arguments\n")
				os.Exit(1)
			}

			err := getAllFilesForCourse(client, *course, *dest, *token)
			if err != nil {
				fmt.Println("Error: ", err)
				os.Exit(1)
			}

		case "announcements": // get announcements for course 

			fs := flag.NewFlagSet("assignments", flag.ExitOnError)
			token := fs.String("token", config.Token, "Canvas API token")
			course := fs.String("courseid", "", "Course id number")
			fs.Parse(os.Args[2:])

			announcements, err := getAnnouncementsForCourse(client, *course, *token)
			if err != nil {
				fmt.Println("Error: ", err)
				os.Exit(1)
			}
			fmt.Print(announcements)

		case "assignments": // get assignments (tasks) for course 
			fs := flag.NewFlagSet("assignments", flag.ExitOnError)
			token := fs.String("token", config.Token, "Canvas API token")
			course := fs.String("courseid", "", "Course id number")
			fs.Parse(os.Args[2:])
		
			assignments, err := getAssignmentsForCourse(client, *course, *token)
			if err != nil {
				fmt.Println("Error: ", err)
				os.Exit(1)
			}
			fmt.Print(assignments)

		case "courses": // get courses for this user
			fs := flag.NewFlagSet("courses", flag.ExitOnError)
			token := fs.String("token", config.Token, "Canvas API token")
			fs.Parse(os.Args[2:])

			courses, err := getCoursesForUser(client, *token)
			if err != nil {
				fmt.Println("Error: ", err)
				os.Exit(1)
			}
			fmt.Print(courses)

		default:
			fmt.Println("Wrong usage. \n-c for courses\n-a for announcements\n-f for files\n-d to see destination folder saved in config")
	}


}

func getCourseNameFromId(client *http.Client, token string, course string) string {
// fires a quick request to get the course name. needed because we are passing the course
// as a number. for a refactor, can simply pick all numbers of courses with names and pick with huh
// but for now will keep this for organizing and pass the param down.
	courses, err := getCoursesForUser(client, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not get courses for user to dervie name")
		return ""
	}
	for _, c := range courses {
		if course == strconv.Itoa(c.CourseId) {
			return c.Name
	}
}
	return "" // no course matches.
}

func getAllFilesForCourse(client *http.Client, course string, dest string, token string) (error) {

	coursename := getCourseNameFromId(client, token, course)

	req, err := http.NewRequest("GET", "https://canvas.cmu.edu/api/v1/courses/" + course +"/modules?per_page=100", nil)
	
	if err != nil {
		fmt.Printf("error with creating request %v\n", err)
	}

	req.Header.Add("Authorization", "Bearer " + token)
	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    fmt.Fprintf(os.Stderr, "unexpected status %s: %s\n", resp.Status, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("error with response %v\n", err)
	}

	defer resp.Body.Close()


	var modules []Module

	err = json.Unmarshal(body, &modules)
	if err != nil {
		fmt.Printf("error with response %v\n", err)
	}

	var moduleitems []ModuleItem
	tok := token
	itemtype := "File"
	for _, m := range modules {
		items, err := GetAndFilterModuleItems(client, m.ItemsUrl, itemtype, tok)
		if err != nil {
			fmt.Printf("%v", err)
		}

		moduleitems = append(moduleitems, items...)
	}
	dt := dest
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	for _, m := range moduleitems {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func()  {<- sem} ()
			err := processFileItem(client, m.Url, dt, tok, coursename)
			if err != nil {
				fmt.Printf("Error downloading.")
			}
	}()
}
	wg.Wait()
	return nil
}


func GetAndFilterModuleItems(client *http.Client, itemsUrl string, itemtype string, token string) ([]ModuleItem, error) {



	var items []ModuleItem

	if err := getJSON(client, itemsUrl+"?per_page=100", token, &items); err != nil {
		return nil, err
	}

	var result []ModuleItem

	for _, m := range(items) {
		if m.Type == itemtype {
			result = append(result, m)
		}
	}

	return result, nil
}


func getFileObjFromUrl(client *http.Client, fileobjurl string, token string) (FileObj, error) {

	var fileobj FileObj
	err := getJSON(client, fileobjurl, token, &fileobj)

	return fileobj, err
}

func DownloadFromFileObj(client *http.Client, fileobj FileObj, dest string, token string, coursename string) (error) {

	if fileobj.LockedForUser == true {
		fmt.Printf("skipping locked file")
		return nil
	}
	os.MkdirAll(filepath.Join(dest, coursename), 0o755)
	
	path := filepath.Join(dest, coursename, fileobj.Title)

	exists, err := fileExists(path)
	if err != nil {
		return err
	}
	if exists {
		fmt.Printf("Skipping existing file %v\n", path)
		return nil
	}

	req, err := http.NewRequest("GET", fileobj.Url, nil)
	
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer " + token)
	fmt.Printf("Downloading %v\n", fileobj.Title)
	resp, err := client.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    return fmt.Errorf("unexpected status %s: %s", resp.Status, body)
	}

	file, err := os.Create(path)
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(path)
		return err	
	}
	return nil
}

func processFileItem(client *http.Client, fileobjurl string, dest string, token string, coursename string) (error) {
	fileobj, err := getFileObjFromUrl(client, fileobjurl, token)
	if err != nil {
		return err
	}


	err = DownloadFromFileObj(client, fileobj, dest, token, coursename)
	if err != nil {
		return fmt.Errorf("Error downloading file %v: %w", fileobj.Title, err)
	}
	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	} else {
		return true, nil
	}

}

func configPath() (string, error) {

	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}

	return filepath.Join(dir, "fetchcanvas", "config.toml"), nil
}

func getConfig(config *Config) (error) {
	// here i need the order: if env passes, use env. if not, check file, no file, send defaults

	// first let's fill the default dir. there's no default token.
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	config.Dir = home

	// file stuff overwriting the defaults
	path, err := configPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "can't find the path to read config file, ", err)
	}
	_, err = toml.DecodeFile(path, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read config file: ", err)
	}

	// env stuff overwriting the file
	dir, dir_ok := os.LookupEnv("DESTINATION_FOLDER")
	tok, tok_ok:= os.LookupEnv("API_TOKEN")

	if dir_ok {
		config.Dir = dir
	}

	if tok_ok {
		config.Token = tok
	}

	
	return nil
}

func getAssignmentsForCourse(client *http.Client, courseid string, token string) ([]Assignment, error) {


	var assignments []Assignment

	err := getJSON(client, "https://canvas.cmu.edu/api/v1/courses/"+courseid+"/assignments?include[]=submission&per_page=100", token, &assignments)

	return assignments, err

}
func getAnnouncementsForCourse(client *http.Client, courseid string, token string) ([]Announcement, error) {


	var announcements []Announcement

	err := getJSON(client, "https://canvas.cmu.edu/api/v1/announcements?context_codes[]=course_"+courseid, token, &announcements)

	return announcements, err

}


func getCoursesForUser(client *http.Client, token string) ([]Course, error) {

	var courses []Course
	err := getJSON(client, "https://canvas.cmu.edu/api/v1/courses?per_page=100", token, &courses)
	return courses, err
}

func getJSON(client *http.Client, url, token string, out any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %s: %s", resp.Status, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
