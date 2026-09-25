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

func main() {

	// 1. get the config file
	var config struct {

		Dest string `toml:"dest"`
		Token string `toml:"token"`
		Course string `toml:"course"`
	}

	_, err:= toml.DecodeFile("/Users/hmz/Documents/projects/gofun/canvas/config.toml", &config)
	if err != nil {
		fmt.Printf("error with decoding config %v\n", err)
	}

	// 2. parse the flags 
	var course = flag.String("courseid", config.Course, "id of the course in canvas url")
	var dest = flag.String("dest", config.Dest, "directory")
	var token = flag.String("token", config.Token, "token")
	flag.Parse()

	if *dest == "" || *course == "" || *token == "" {
		fmt.Printf(" courseid, destination folder and token need to be specified either on config file or as cli arguments\n")
		os.Exit(1)
	}
	// to be removed later, just for ease
	fmt.Printf("dest used is %s\n", *dest)
	fmt.Printf("course used is %s\n", *course)

	// 3. now, try the http connection
	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
		},
	}

	req, err := http.NewRequest("GET", "https://canvas.cmu.edu/api/v1/courses/" + *course +"/modules?per_page=100", nil)
	
	if err != nil {
		fmt.Printf("error with creating request %v\n", err)
		os.Exit(1)
	}

	req.Header.Add("Authorization", "Bearer " + *token)
	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
	    body, _ := io.ReadAll(resp.Body)
	    fmt.Fprintf(os.Stderr, "unexpected status %s: %s\n", resp.Status, body)
	    os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("error with response %v\n", err)
		os.Exit(1)
	}

	defer resp.Body.Close()


	var modules []Module

	err = json.Unmarshal(body, &modules)
	if err != nil {
		fmt.Printf("error with response %v\n", err)
		os.Exit(1)
	}

	var moduleitems []ModuleItem
	tok := *token
	itemtype := "File"
	for _, m := range modules {
		items, err := GetAndFilterModuleItems(client, m.ItemsUrl, itemtype, tok)
		if err != nil {
			fmt.Printf("%v", err)
			os.Exit(1)
		}

		moduleitems = append(moduleitems, items...)
	}
	dt := *dest
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
