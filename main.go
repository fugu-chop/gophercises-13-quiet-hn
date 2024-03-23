package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"quiet-hn/hn"
)

func main() {
	// parse flags
	var port, numStories int
	flag.IntVar(&port, "port", 3000, "the port to start the web server on")
	flag.IntVar(&numStories, "num_stories", 30, "the number of top stories to display")
	flag.Parse()

	tpl := template.Must(template.ParseFiles("./index.gohtml"))

	http.HandleFunc("/", handler(numStories, tpl))

	// Start the server
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handler(numStories int, tpl *template.Template) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var client hn.Client
		// Assumption: We will get enough ids from TopItems to ensure 30 stories
		ids, err := client.TopItems()
		if err != nil {
			http.Error(w, "Failed to load top stories", http.StatusInternalServerError)
			return
		}
		// This is the point that is best to introduce concurrency
		// I probably want to introduce a function so as to leave the client untouched
		// Pass in a pointer to a client and a slice of items
		// Let the function handle iteration
		// Spawn goroutines. No need for channels?
		// The client via #TopItems will fetch ~= 450 items
		// We pull out 30 that match stories

		// Fetching stories is the slow bit to use concurrency with
		// Basically we want to keep spawning goroutines until our slice of items
		// is len(30) of appropriate stories

		// Keep in mind we need to preserve "order"
		// Order appears to be by ID
		// We might have to do some sorting post fetch

		// How might caching work?
		// Set a bool flag for making requests - stop requests from going through
		// Spin up a goroutine that's checking a duration
		// Once the duration is hit, flip the flag to false, which should allow a request to go through

		// var stories []item
		// for _, id := range ids {
		// 	hnItem, err := client.GetItem(id)
		// 	if err != nil {
		// 		continue
		// 	}
		// 	item := parseHNItem(hnItem)
		// 	if isStoryLink(item) {
		// 		stories = append(stories, item)
		// 		if len(stories) >= numStories {
		// 			break
		// 		}
		// 	}
		// }
		stories := fetchStories(&client, ids, numStories)

		data := templateData{
			Stories: stories,
			Time:    time.Now().Sub(start),
		}
		err = tpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Failed to process the template", http.StatusInternalServerError)
			return
		}
	})
}

func isStoryLink(item item) bool {
	return item.Type == "story" && item.URL != ""
}

func parseHNItem(hnItem hn.Item) item {
	ret := item{Item: hnItem}
	url, err := url.Parse(ret.URL)
	if err == nil {
		ret.Host = strings.TrimPrefix(url.Hostname(), "www.")
	}
	return ret
}

// item is the same as the hn.Item, but adds the Host field
type item struct {
	hn.Item
	Host string
}

type templateData struct {
	Stories []item
	Time    time.Duration
}

// We need to block the main goroutine from returning early
func fetchStories(client *hn.Client, ids []int, numStories int) []item {
	var stories []item

	signal := make(chan bool)
	defer close(signal)

	for _, id := range ids {
		go func(id int) {
			hnItem, err := client.GetItem(id)
			if err != nil {
				return
			}
			item := parseHNItem(hnItem)
			if isStoryLink(item) {
				stories = append(stories, item)
				if len(stories) >= numStories {
					<-signal
					return
				}
			}
		}(id)
	}

	signal <- true

	return stories
}
