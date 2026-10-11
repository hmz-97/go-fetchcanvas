package main

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"io/fs"
	"errors"
	"flag"
	"net/http"
	"sync"
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

	Dest string `toml:"dest"`
	Token string `toml:"token"`
	Course string `toml:"course"`
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
		case "directory": // list directory
			if config.Dest == "" {
				fmt.Println("No destination dir in config")
				os.Exit(1)
			}
			fmt.Println("Destination directory is " +config.Dest)

		case "files": // get files for course


			fs := flag.NewFlagSet("assignments", flag.ExitOnError)
			token := fs.String("token", config.Token, "Canvas API token")
			course := fs.String("courseid", config.Course, "Course id number")
			dest := fs.String("dest", config.Dest, "directory")
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
			course := fs.String("courseid", config.Course, "Course id number")
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
			course := fs.String("courseid", config.Course, "Course id number")
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

func getAllFilesForCourse(client *http.Client, course string, dest string, token string) (error) {

	// to be removed later, just for ease
	fmt.Printf("dest used is %s\n", dest)
	fmt.Printf("course used is %s\n", course)

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
			err := processFileItem(client, m.Url, dt, tok)
			if err != nil {
				fmt.Printf("Error downloading.")
			}
	}()
}
	wg.Wait()
	return nil
}


func GetAndFilterModuleItems(client *http.Client, itemsUrl string, itemtype string, token string) ([]ModuleItem, error) {


	req, err := http.NewRequest("GET", itemsUrl, nil)
	
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer " + token)
	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    return nil, fmt.Errorf("unexpected status %s: %s", resp.Status, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var moduleitems []ModuleItem

	err = json.Unmarshal(body, &moduleitems)
if err != nil {
		return nil, err	
	}

	var result []ModuleItem

	for _, m := range(moduleitems) {
		if m.Type == itemtype {
			result = append(result, m)
		}
	}

	return result, nil
}


func GetFileObjFromUrl(client *http.Client, fileobjurl string, token string) (FileObj, error) {
	req, err := http.NewRequest("GET", fileobjurl, nil)
	
	if err != nil {
		return FileObj{}, err
	}

	req.Header.Add("Authorization", "Bearer " + token)
	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    return FileObj{}, fmt.Errorf("unexpected status %s: %s", resp.Status, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return FileObj{}, err
	}

	defer resp.Body.Close()

	var fileobj FileObj

	err = json.Unmarshal(body, &fileobj)
	if err != nil {
		return FileObj{}, err	
	}


	return fileobj, nil
}

func DownloadFromFileObj(client *http.Client, fileobj FileObj, dest string, token string) (error) {

	if fileobj.LockedForUser == true {
		fmt.Printf("skipping locked file")
		return nil
	}
	os.MkdirAll(dest, 0o755)
	
	path := filepath.Join(dest, fileobj.Title)

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

func processFileItem(client *http.Client, fileobjurl string, dest string, token string) (error) {
	fileobj, err := GetFileObjFromUrl(client, fileobjurl, token)
	if err != nil {
		return err
	}


	err = DownloadFromFileObj(client, fileobj, dest, token)
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

	path, err := configPath()
	_, err = toml.DecodeFile(path, &config)
	if err != nil {
		return err
	}
	return nil
}
func getAssignmentsForCourse(client *http.Client, courseid string, token string) ([]Assignment, error) {


	req, err := http.NewRequest("GET", "https://canvas.cmu.edu/api/v1/courses/" + courseid + "/assignments/", nil)
	req.Header.Add("Authorization", "Bearer " + token)

	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    return nil, fmt.Errorf("unexpected status %s: %s", resp.Status, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var assignments []Assignment

	err = json.Unmarshal(body, &assignments)
	if err != nil {
		return nil, err	
	}

	return assignments, nil

}
func getAnnouncementsForCourse(client *http.Client, courseid string, token string) ([]Announcement, error) {


	req, err := http.NewRequest("GET", "https://canvas.cmu.edu/api/v1/announcements?context_codes[]=course_" + courseid, nil)
	req.Header.Add("Authorization", "Bearer " + token)

	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    return nil, fmt.Errorf("unexpected status %s: %s", resp.Status, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var announcements []Announcement

	err = json.Unmarshal(body, &announcements)
	if err != nil {
		return nil, err	
	}

	return announcements, nil

}


func getCoursesForUser(client *http.Client, token string) ([]Course, error) {

	req, err := http.NewRequest("GET", "https://canvas.cmu.edu/api/v1/courses/", nil)
	req.Header.Add("Authorization", "Bearer " + token)

	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    return nil, fmt.Errorf("unexpected status %s: %s", resp.Status, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var courses []Course

	err = json.Unmarshal(body, &courses)
	if err != nil {
		return nil, err	
	}

	return courses, nil
}
