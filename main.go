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

var (
	shouldFetch   = true
	cachedStories []item
)

func main() {
	// parse flags
	var port, numStories, cacheTime int
	flag.IntVar(&port, "port", 3000, "the port to start the web server on")
	flag.IntVar(&numStories, "num_stories", 30, "the number of top stories to display")
	flag.IntVar(&cacheTime, "cache_time", 5, "the number of minutes that stories will be cached")
	flag.Parse()

	tpl := template.Must(template.ParseFiles("./index.gohtml"))

	http.HandleFunc("/", handler(numStories, tpl, cacheTime))

	// Start the server
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handler(numStories int, tpl *template.Template, cacheTime int) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var client hn.Client
		// Assumption: We will get enough ids from TopItems to ensure 30 stories
		ids, err := client.TopItems()
		if err != nil {
			http.Error(w, "Failed to load top stories", http.StatusInternalServerError)
			return
		}

		// Avoid unnecessary requests if time hasn't elapsed
		if shouldFetch {
			// stories := fetchStoriesSync(&client, ids, numStories)
			shouldFetch = false
			unorderedStories := fetchStoriesAsync(&client, ids)
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
			cachedStories = orderedItems[:numStories]
		}

		cacheLength := time.Minute * time.Duration(cacheTime)
		time.AfterFunc(cacheLength, func() {
			shouldFetch = true
		})

		data := templateData{
			Stories: cachedStories,
			Time:    time.Since(start),
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

// WaitGroups feel like overkill versus channels, but the
// problem is that there is no clear time at which we should
// terminate the channel due to the different speeds at which
// stories can be parsed.
func fetchStoriesAsync(client *hn.Client, ids []int) []orderedItem {
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
