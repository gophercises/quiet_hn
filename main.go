package main

import (
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"quiet_hn/hn"
)

func main() {
	// parse flags
	var port, numStories int
	flag.IntVar(&port, "port", 3000, "the port to start the web server on")
	flag.IntVar(&numStories, "num_stories", 30, "the number of top stories to display")
	flag.DurationVar(&cacheExpiration, "duration", 10*time.Second, "the time between cache refreshes")
	tick = time.Tick(cacheExpiration)
	flag.Parse()

	for i := 0; i < 10; i++ {
		go startWorkers()
	}

	tpl := template.Must(template.ParseFiles("./index.gohtml"))

	go refreshCache(numStories)
	http.HandleFunc("/", handler(numStories, tpl))
	log.Println("starting server")

	// Start the server
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handler(numStories int, tpl *template.Template) http.HandlerFunc {
	stories, err := getTopStories(numStories)
	if err != nil {
		// yeah yeah....
		panic(err)
	}
	cacheA = stories[:]

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		stories, err := getCachedStories()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := templateData{
			Stories: stories,
			Time:    time.Since(start),
		}
		err = tpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Failed to process the template", http.StatusInternalServerError)
			return
		}
	})
}

var (
	cacheA          []item
	tick            <-chan time.Time
	jobs            = make(chan job, numStories)
	resultCh        = make(chan result, 100)
	numStories      int
	cacheExpiration time.Duration
)

func refreshCache(numStories int) {
	for {
		<-tick
		log.Println("refreshing")
		stories, err := getTopStories(numStories)
		if err != nil {
			continue
		}
		a := uintptr(unsafe.Pointer(&cacheA))
		for atomic.SwapUintptr(&a, uintptr(unsafe.Pointer(&stories))) == a {
		}
	}
}

func getCachedStories() ([]item, error) {
	return cacheA, nil
}

func getTopStories(numStories int) ([30]item, error) {
	var stories [30]item
	now := time.Now()
	defer log.Println("getTopStories took", time.Since(now))
	var client hn.Client
	ids, err := client.TopItems()
	if err != nil {
		return stories, errors.New("Failed to load top stories")
	}
	var (
		at   = 0
		need int
	)

	for {
		need = (numStories - at) * 5 / 4
		strs := getStories(ids[at : at+need])
		copy(stories[at:at+len(strs)], strs[:])
		at += len(strs)
		if at >= numStories {
			break
		}
	}
	return stories, nil
}

type result struct {
	idx  int
	item item
	err  error
}

type job struct {
	id, idx int
}

func startWorkers() {
	var client hn.Client
	for job := range jobs {
		hnItem, err := client.GetItem(job.id)
		if err != nil {
			resultCh <- result{idx: job.idx, err: err}
		}
		resultCh <- result{idx: job.idx, item: parseHNItem(hnItem)}
	}
}

func getStories(ids []int) []item {
	for i := 0; i < len(ids); i++ {
		jobs <- job{idx: ids[i], id: i}
	}

	results := make([]result, len(ids))
	for i := 0; i < len(ids); i++ {
		results[i] = <-resultCh
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].idx < results[j].idx
	})

	var stories []item
	for _, res := range results {
		if res.err != nil {
			continue
		}
		if isStoryLink(res.item) {
			stories = append(stories, res.item)
		}
	}
	return stories
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
