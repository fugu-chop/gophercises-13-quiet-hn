package main

import (
	"cmp"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
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
		unorderedStories := fetchStories(&client, ids)
		slices.SortFunc(unorderedStories, func(a, b orderedItem) int {
			return cmp.Compare(a.order, b.order)
		})

		var orderedItems []item
		for _, story := range unorderedStories {
			item := item{
				story.Item,
				story.Host,
			}

			orderedItems = append(orderedItems, item)
		}
		stories := orderedItems[:numStories]

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

type orderedItem struct {
	item
	order int
}

type templateData struct {
	Stories []item
	Time    time.Duration
}

func captureOrder(ids []int) map[int]int {
	orderMap := map[int]int{}
	for idx, id := range ids {
		orderMap[id] = idx
	}

	return orderMap
}

// The ids won't always be in the same order when provided
// to this function. Need to create a new type in order to sort
// by index
func fetchStories(client *hn.Client, ids []int) []orderedItem {
	order := captureOrder(ids)

	var stories []orderedItem
	var wg sync.WaitGroup
	wg.Add(len(ids))

	for _, id := range ids {
		go func(id int, wg *sync.WaitGroup) {
			defer wg.Done()
			hnItem, err := client.GetItem(id)
			if err != nil {
				return
			}
			item := parseHNItem(hnItem)
			if isStoryLink(item) {
				orderedItem := orderedItem{
					item:  item,
					order: order[item.ID],
				}
				stories = append(stories, orderedItem)
			}
		}(id, &wg)
	}

	wg.Wait()

	return stories
}
