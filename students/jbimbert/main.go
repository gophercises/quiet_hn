package main

import (
	"Gophercizes/quiet_hn/hn"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type cache struct {
	maxSize int // max number of items in stories
	curSize int // current number of items in stories
	stories []item
	timeout time.Duration
	mutx    sync.Mutex
}

var cach cache

func main() {
	// parse flags
	var port, numStories, batchSize, cachTimeout1, cachSize1 int
	var mode string
	flag.IntVar(&port, "port", 3000, "the port to start the web server on")
	flag.IntVar(&numStories, "num_stories", 30, "the number of top stories to display")
	flag.StringVar(&mode, "mode", "wg", "the mode to use : wg (wait group), ch (channel), none (no concurrency)")
	flag.IntVar(&batchSize, "batch_size", 10, "the number of routines to run simultaneously")
	flag.IntVar(&cachTimeout1, "timeout", 10, "the cache timeout in seconds")
	flag.IntVar(&cachSize1, "cach_size", 40, "the number of items in cache")
	flag.Parse()

	t, err := time.ParseDuration(fmt.Sprintf("%ds", cachTimeout1))
	if err != nil {
		log.Fatal(err)
	}

	cach.maxSize = cachSize1
	cach.timeout = t
	cach.stories = make([]item, cach.maxSize)

	fmt.Println("number of stories to retrieve : ", numStories)
	fmt.Println("display port : ", port)
	fmt.Println("Concurrency mode : ", mode)
	fmt.Println("cach timeout : ", cach.timeout)
	fmt.Println("cach size : ", cach.maxSize)
	if mode != "none" {
		fmt.Println("Number of routines : ", batchSize)
		fmt.Println("Batch size : ", batchSize)
	}

	tpl := template.Must(template.ParseFiles("./index.gohtml"))

	http.HandleFunc("/", handler(numStories, tpl, mode, batchSize))

	// Start the server
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handler(numStories int, tpl *template.Template, mode string, batchSize int) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var client hn.Client
		ids, err := client.TopItems()
		if err != nil {
			http.Error(w, "Failed to load top stories", http.StatusInternalServerError)
			return
		}
		cach.purge()
		var stories []item
		if mode == "none" {
			stories = append(stories, getStories(ids, numStories)...)
		} else {
			idx := 0
			for len(stories) < numStories {
				l := len(stories)
				nmax := int(float32(numStories-l) * 1.3)
				n := min(nmax, batchSize)
				var newStories []item
				if mode == "wg" {
					newStories = getStoriesWithWaitGroup(idx, ids[idx:idx+n])
				} else {
					newStories = getStoriesWithChannel(idx, ids[idx:idx+n])
				}
				idx += n
				if len(newStories) == 0 {
					continue
				}
				stories = append(stories, newStories...)
			}
		}
		sort.Slice(stories, func(i, j int) bool {
			return stories[i].id < stories[j].id
		})
		data := templateData{
			Stories: stories[:numStories],
			Time:    time.Now().Sub(start),
		}
		err = tpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Failed to process the template", http.StatusInternalServerError)
			return
		}
		cach.add(stories[:numStories])
	})
}

// look for an item in the cache
func (c *cache) find(id int) (item, bool) {
	for i := 0; i < c.curSize; i++ {
		if c.stories[i].Item.ID == id {
			return c.stories[i], true
		}
	}
	return item{}, false
}

// Add items of the stories that are not already in the cache
func (c *cache) add(stories []item) {
	size := c.curSize
	for _, story := range stories {
		found := false
		for i := 0; i < size; i++ {
			if c.stories[i].id == story.id {
				found = true
				break
			}
		}
		if !found {
			if c.curSize < c.maxSize {
				c.mutx.Lock()
				c.stories[c.curSize] = story
				c.curSize++
				c.mutx.Unlock()
			}
			if c.curSize >= c.maxSize {
				return
			}
		}
	}
}

// Remove items that are in time out
func (c *cache) purge() {
	p := make([]item, c.maxSize)
	t := time.Now()
	n := 0
	for i := 0; i < c.curSize; i++ {
		if t.Sub(c.stories[i].t) < c.timeout {
			p[n] = c.stories[i]
			n++
		}
	}
	c.mutx.Lock()
	defer c.mutx.Unlock()
	c.stories = p
	c.curSize = n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Use blocking channels with go routines
func getStoriesWithChannel(idx int, ids []int) []item {
	var stories []item
	c := make(chan item)
	n := 0
	for i, id := range ids { // Look up the cache first
		if it, found := cach.find(id); found {
			stories = append(stories, it)
			if len(stories) >= len(ids) {
				break
			}
			continue
		}
		n++
		go func(i, id int) {
			var client hn.Client
			hnItem, err := client.GetItem(id)
			if err != nil {
				log.Println(err)
			}
			itm := parseHNItem(hnItem, i+idx)
			if !isStoryLink(itm) {
				itm.id = -1
			}
			c <- itm
		}(i, id)
	}
	for i := 0; i < n; i++ { // we need to know the exact number of items in the blocking channel
		itm := <-c
		if itm.ID != -1 {
			stories = append(stories, itm)
		}
	}
	return stories
}

// Use wait groupd with go routines
func getStoriesWithWaitGroup(idx int, ids []int) []item {
	stories := make([]item, len(ids))
	var p []int
	n := 0
	for _, id := range ids { // Look up the cache first
		it, found := cach.find(id)
		if found {
			stories[n] = it
			n++
			if n >= len(ids) {
				return stories
			}
			continue
		} else {
			if n+len(p) >= len(ids) {
				break
			}
			p = append(p, id)
		}
	}

	var wg sync.WaitGroup
	temp := make([]item, len(p)) // use a temporary array to avoid data races
	for i, id := range p {       // not found in cache, send a request
		wg.Add(1)
		go func(i, id int) {
			var client hn.Client
			hnItem, err := client.GetItem(id)
			if err != nil {
				log.Println(err)
			}
			item := parseHNItem(hnItem, i+idx)
			temp[i] = item
			wg.Done()
		}(i, id)
	}
	wg.Wait()

	for _, it := range temp { // add the temp to the stories if it is a story
		if isStoryLink(it) {
			stories[n] = it
			n++
		}
	}
	return stories
}

// Use no go routines
func getStories(ids []int, numstories int) []item {
	var client hn.Client
	var stories []item
	for i, id := range ids { // Look up the cache first
		if it, found := cach.find(id); found {
			stories = append(stories, it)
			if len(stories) >= numstories {
				break
			}
			continue
		}
		hnItem, err := client.GetItem(id) // not found in cache, send a request
		if err != nil {
			log.Println(err)
		}
		item := parseHNItem(hnItem, i)
		if isStoryLink(item) {
			stories = append(stories, item)
			if len(stories) >= numstories {
				break
			}
		}
	}
	return stories
}

func isStoryLink(item item) bool {
	return item.Type == "story" && item.URL != ""
}

func parseHNItem(hnItem hn.Item, id int) item {
	ret := item{Item: hnItem}
	url, err := url.Parse(ret.URL)
	if err == nil {
		ret.Host = strings.TrimPrefix(url.Hostname(), "www.")
		ret.id = id
		ret.t = time.Now()
	}
	return ret
}

// item is the same as the hn.Item, but adds the Host field
type item struct {
	hn.Item
	Host string
	id   int
	t    time.Time
}

type templateData struct {
	Stories []item
	Time    time.Duration
}
