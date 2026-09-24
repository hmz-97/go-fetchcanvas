package main

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"flag"
	"net/http"
	"os"
	"io"
	"encoding/json"
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
	fmt.Printf("token used is %s\n", *token)

	// 3. now, try the http connection
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://canvas.cmu.edu/api/v1/courses/" + *course +"/modules", nil)
	
	if err != nil {
		fmt.Printf("error with creating request %v\n", err)
		os.Exit(1)
	}

	req.header.add("authorization", "bearer " + *token)
	resp, err := client.do(req)

	if err != nil {
		fmt.printf("error with response %v\n", err)
		os.exit(1)
	}

	body, err := io.readall(resp.body)
	if err != nil {
		fmt.printf("error with response %v\n", err)
		os.exit(1)
	}

	defer resp.body.close()


	var modules []Module

	err = json.unmarshal(body, &modules)
	if err != nil {
		fmt.printf("error with response %v\n", err)
		os.exit(1)
	}

	fmt.printf("found modules %v", modules)

	var moduleitems []ModuleItem
	tok := *token
	itemtype := "File"
	for _, m := range modules {
		items, err = GetAndFilterModuleItems(m.ItemsUrl, itemtype, tok)
		if err != nil {
			fmt.Printf("%v", err)
			os.Exit(1)
		}

		moduleitems = append(moduleitems, items)
	}

	fmt.printf("found modules %v", moduleitems)

}

func GetAndFilterModuleItems(string itemsUrl, string itemtype, string token) ([]ModuleItem, error) {


	client := &http.Client{}
	req, err := http.NewRequest("GET", itemsUrl, nil)
	
	if err != nil {
		return nil, err
	}

	req.header.add("authorization", "bearer " + token)
	resp, err := client.do(req)

	if err != nil {
		return nil, err
	}

	body, err := io.readall(resp.body)
	if err != nil {
		return nil, err
	}

	defer resp.body.close()

	var moduleitems []ModuleItem

	err = json.unmarshal(body, &moduleitems)
	if err != nil {
		return nil, err	
	}

	var result []ModuleItem

	for i, m in range(result) {
		if m.Type == "File" {
			result.append(m)
		}
	}

	return result, nil
}
