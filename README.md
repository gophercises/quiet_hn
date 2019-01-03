# Exercise #13: Quiet HN

[![exercise status: released](https://img.shields.io/badge/exercise%20status-released-green.svg?style=for-the-badge)](https://gophercises.com/exercises/quiet_hn)

## Exercise details

One of the most common approaches to learning a new language is to rebuild things you know. As a result, you will often find tons of clones for websites like Twitter, Hacker News (HN), Pinterest, and countless others. The clones are rarely better than the original, but that isn't the point. The point is to build something you know so that you can eliminate a lot of the guesswork and uncertainty that comes with building something new.

In this exercise we aren't going to be building a clone from scratch, but we are going to take a relatively simple [Hacker News](https://news.ycombinator.com) clone (called [Quiet Hacker News](https://github.com/tomspeak/quiet-hacker-news)) and use it to explore concurrency and caching in Go. That said, you are welcome to build your own HN clone before moving forward with the exercise by reading what the current one does below and writing a similar server.


### The existing code

The repository currently has a minimal version of the Quiet Hacker News application. The goal of the application is to take the top stories from Hacker News and remove things like comments, poster identity, and job postings. To do this, the application first starts a web server, then on every web request it will look up the top 30 stories on HN via the API that:

1. Have a `Type` of `"story"`. This filters out all job postings and other types of items.
2. Have a `URL` instead of `Text`. This filters out things like Ask HN questions and other discussions.

What this means is that the web application won't always look up exactly 30 stories, but might instead need to look up 32 or 33 stories if a few of the first 30 were job postings, Ask HN questions, or something other than a story with a link to another website.

The application then renders all of those stories, along with some footer text that logs how long it took to render the webpage.

![example rendering of the Quiet HN page](https://www.dropbox.com/s/nexh2oql60a25df/Screenshot%202018-04-02%2017.34.01.png?dl=0&raw=1)

### Concurrency

Rather than focusing on how to build this application we are going to look at ways to add both concurrency and caching in order to speed up the application. The first - concurrency - will be explored because it is a common reason to want to check out Go, and it is nice to get a feel for how it works in the language. The second - caching - is important because this is actually one of the easiest and most effective ways to speed up our application, and even outperforms our concurrency changes.

For the first phase of this exercise, explore the existing code and figure out a way to retrieve the stories concurrently. At first just try to get something working, but once you have that try to make sure you meet all of the following criteria:

#### 1. Stories MUST retain their original order

When retrieving stories concurrently it is possible that you will get results back for a story in position #5 before getting a response about the #3 story. Regardless of the order you hear back, your stories should always be in the correct order - the same order they are in the current version of the code. The only way a story should change positions when compared to Hacker News is if an earlier story was filtered out, and even then the index should be changed but stories shouldn't swap positions randomly.

#### 2. Make sure you ALWAYS print out 30, and only 30, stories

When interacting with the HN API you might retrieve stories that need to be filtered. As a result, you can't just get the first 30 stories and then render your page, but you instead need to make sure you find the first 30 that don't get filtered. Doing this without concurrency is easy, but doing this both with concurrency, and while retaining the original order of the stories, can make it tricky.

The first approach I would recommend is to always get a few extra stories to account for filtered stories. Eg if `numStories` is set to `30`, maybe we should always retrieve `1.25 * 30` stories concurrently to account for filtered stories. We obviously can't *always* count on this working, but it should work a majority of the time.

After that is working, try to figure out a way to ensure you always get at least 30 stories regardless of how many might be filtered out. As I stated earlier, this can be a little trickier with concurrency and while retaining the original order of the stories so take your time and try a few approaches out. Try to weigh the pros and cons of each approach and see if you can find any edge cases where your solution would be incorrect.

*Note: If you change the value of the `numStories` flag then obviously 30 will be a different value, but the same general rule still applies - always render the correct amount of stories.*

If a story is in position #3 on HN, it should be in that same position with your concurrent version of this application. This means that even though your API request for story #4 may finish BEFORE story #3

**Warning:** *Each of these two rules are much easier to implement independently than they are together, so if you get stuck or frustrated don't worry - it isn't exactly a simple problem to approach.*

### Caching

In addition to adding concurrency, add caching to the application. Your cache should store the results of the top `numStories` stories so that subsequent web requests don't require additional API calls, but that cache should expire at some point after which time more API calls will be needed to update the cache.

How you implement this is up to you, but you should definitely consider the fact that many web requests can be processed at the same time, so you may need to take race conditions into consideration. A great way to test this is the [-race flag](https://blog.golang.org/race-detector).


## Bonus

Experiment with how many goroutines you use. For instance, some of you will code this by creating a goroutine for each item you are retrieving from the HN API, while others will use maybe a small set of goroutines and have them work through a list of item IDs that need retrieved. Try both approaches and see how they compare.

*Note: You can limit your workers via channels, or with something like the [x/sync/semaphore](https://godoc.org/golang.org/x/sync/semaphore) package.*

You can also look into ways to improve your cache. For instance, imagine we have a cache that we invalidate every 15 minutes, at which point we will replace all the values in it when we receive the next web request. This means that the next web request will be slow because it has to wait on us to repopulate the cache. One way to improve this experience is to always keep a valid cache, which can be done by creating the new cache BEFORE the old one expires, then rotating which cache we use. Now if we were to update and rotate the caches every 10 minutes, it is very unlikely that our currently in-use cache will ever exceed the 15 minute deadline and our users won't ever see an noticeable slowdown. 
