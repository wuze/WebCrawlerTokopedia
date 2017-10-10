//================================================================================
/*This code is a crawler and scrapper program.
It crawls the given domain and extract the required data from the web pages.
Currently, it is defined to extract very specific requirements	such as 

	1. Product-ID
	2. Product-URL
	3. Product-video URLs 

It writes the information in a TSV (Tab Sepearated File) file.
It keeps track of web pages it visited by writting it to a file.

*/
//================================================================================

package main

import (

	"golang.org/x/net/html"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"context"
	"bytes"
	"flag"
	"sync"
	"time"
	"fmt"
	"log"
	"os"
	
	cdp "github.com/knq/chromedp"
	cdpr "github.com/knq/chromedp/runner"
	
)
//================================================================================
//================================================================================

var (
	baseurl, productFile, urlFile string
	wg sync.WaitGroup

	// Command-line flags
	seed		= flag.String("seed", "https://www.tokopedia.com/", "seed URL")
	cancelAfter	= flag.Duration("cancelafter", 0, "automatically cancel the fetchbot after a given time")
	cancelAtURL	= flag.String("cancelat", "", "automatically cancel the fetchbot at a given URL")
	stopAfter	= flag.Duration("stopafter", 0, "automatically stop the fetchbot after a given time")
	stopAtURL	= flag.String("stopat", "", "automatically stop the fetchbot at a given URL")
	memStats	= flag.Duration("memstats", 5 * time.Minute, "display memory statistics at a given interval")
	headLess	= flag.Bool("headless", true, "Run the CDP in headless mode.")
)
//================================================================================
//================================================================================

// main runs the fetcher and different go routines.
func main() {

	start := time.Now()
	flag.Parse()

	u, err := url.Parse(*seed)
	check(err, "Error in parsing the seed url")
	log.Println("The URL: ", u)

	if (*headLess) {
		log.Println("Headless mode is enabled. CDP will run in Headless mode.")
	}
	baseurl = u.String()
	urlProcessor	:= make(chan string)
	done		:= make(chan bool)

	go processURL(urlProcessor, done)

	urlProcessor <- u.String()

	// First mem stat print must be right after creating the fetchbot
	if *memStats > 0 {

		// Print starting stats
		printMemStats()

		// Run at regular intervals
		runMemStats(*memStats)

		// On exit, print ending stats after a GC
		defer func() {

			runtime.GC()
			printMemStats()
		}()
	}

	// if a stop or cancel is requested after some duration, launch the goroutine
	// that will stop or cancel.
	if *stopAfter > 0 || *cancelAfter > 0 {

		after		:= *stopAfter
		stopFunc	:= true

		if *cancelAfter != 0 {

			after		= *cancelAfter
			stopFunc	= true
		}

		go func() {

			c := time.After(after)
			<-c

			log.Println("The given timeout has occured. Exiting the program...")
			done <- stopFunc
		}()
	}

	<-done
	
	log.Println(strings.Repeat("=", 72) + "\n")
	log.Println("\n\nCompleted Crawling & Scrapping the Domain:\n", baseurl)

	// Print the product and URLs file details.
	outFileDetails()
	log.Println(strings.Repeat("=", 72) + "\n")

	elapsed := time.Since(start)
	log.Printf("Time required to complete: %s\n", elapsed)

}
//================================================================================
//================================================================================

// DoExtract runs the extractor and get the required data from the given webpage.
func DoExtract(url string){

	time.Sleep(2 * time.Millisecond)

		wg.Add(1)
		go func(){

			defer wg.Done()
			if *headLess {

				//log.Println("In the headless if now")
				DoCDPHeadless(url)
				runtime.GC()
			} else {
			
				DoCDP(url)
				runtime.GC()
			}
		}()
		wg.Wait();
}

// processURL checks the url is already visited or not.
// If not visited already, then set map = true and explore page for more links.
func processURL(urlProcessor chan string, done chan bool) {

	visited := make(map[string]bool)
	for {
		select {
		case url := <-urlProcessor:
			if _, ok := visited[url]; ok {
				continue
			} else {
				visited[url] = true

				go exploreURL(url, urlProcessor)
				DoExtract(url)
				runtime.GC()
			}

		case <-time.After(15 * time.Second):
			log.Printf("Explored %d pages\n", len(visited))
			done <- true
			
		}
	}
}

// exploreURL does HTTP GET and tokenize the response
func exploreURL(url string, urlProcessor chan string) {

	log.Printf("\n\nVisiting %s.\n\n", url)

	resp, err := http.Get(url)
	if err != nil {
		log.Println(err)
		return
	}

	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return
		}

		if tt == html.StartTagToken {
			t := z.Token()

			if t.Data == "a" {
				for _, a := range t.Attr {
					if a.Key == "href" {

						// if link is within baseurl
						if strings.HasPrefix(a.Val, baseurl) {

							// Filter unwanted URLs
							urlProcessor <- filterURL(a.Val)
							//urlProcessor <- a.Val
						}
					}
				}
			}
		}
	}
}

// filterURL filter out the unwanted URLs.
// Currently baseUrl + level 2 is considered to avoid review and other unwanted sub-links.
func filterURL(url string) string {
	var fUrl string
	urlArray := strings.Split(url, "/")

	switch {
	case len(urlArray) > 5:
		
		// Check for nil parts. Just to be sure.
		if urlArray[3] != "" && urlArray[4] != "" {
						
			//fmt.Println("URL is: ", urlArray[0] + "//" + urlArray[1] + urlArray[2] + "/" + urlArray[3] + "/" + urlArray[4])
			fUrl = urlArray[0] + "//" + urlArray[1] + urlArray[2] + "/" + urlArray[3] + "/" + urlArray[4]
		}
	default:
		fUrl = url
	}
	
return fUrl
}

//================================================================================
//================================================================================

// DoCDPHeadless extract all the required information from the given URL.
// It uses chromedp package to complete all the tasks. It uses the headless mode.
func DoCDPHeadless(url string){
	// create context
	ctxt, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create chrome instance with cmd line options disable-web-security & headless.

	path := getOS()
	c, err := cdp.New(ctxt, cdp.WithRunnerOptions(
		cdpr.Headless(path, 9222),
		cdpr.Flag("headless", true),
		cdpr.Flag("disable-web-security", true),
		cdpr.Flag("no-first-run", true),
		cdpr.Flag("no-default-browser-check", true),
		cdpr.Flag("disable-gpu", true),
	)) //, cdp.WithLog(log.Printf))

	check(err, "\nError in creating new cdp instance")
	
	// run task list
	var buf, buf1 []byte
	var pId, pUrl string
	
	// Check for the existence of the webyclip-widget-3 on the page
	err = c.Run(ctxt, isPresent(url, &buf1))
	if err != nil && strings.Contains(fmt.Sprint(err), "Uncaught"){
		return
	} else {

		check(err, "Error in Run method of cdp")
	}

	if (len(buf1) == 0) || (bytes.EqualFold([]byte("0"), buf1)){

		log.Println("No webyclip-widget-3 on page:\n ", url)

		// shutdown chrome
		err = c.Shutdown(ctxt)
		check(err, "Error in shutting down chrome")
		return

	} else { 
	
		// Exit the code if "webyclip-widget-3" is not present.
		err = c.Run(ctxt, getProductInfo(url, `#webyclip-widget-3`, &buf, &pId, &pUrl, &url))
		check(err, "Error in Run method of cdp")

		// shutdown chrome
		err = c.Shutdown(ctxt)
		check(err, "Error in shutting down chrome")
	
		pLinks		:= getVideoLinks(buf)
		record		:= fmt.Sprint(pId + "\t" + pUrl + "\t" + pLinks)
		WriteToFile(record)
	}
}

// DoCDP extract all the required information from the given URL.
// It uses chromedp package to complete all the tasks.
func DoCDP(url string) {
	
	// create context
	ctxt, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create chrome instance
	//c, err := cdp.New(ctxt, cdp.WithLog(log.Printf), cdp.WithRunnerOptions(cdpr.Flag("disable-web-security", true)))

	c, err := cdp.New(ctxt, cdp.WithRunnerOptions(cdpr.Flag("disable-web-security", true)))
	check(err, "\nError in creating new cdp instance")
	
	// run task list
	var buf, buf1 []byte
	var pId, pUrl string
	
	// Check for the existence of the webyclip-widget-3 on the page
	err = c.Run(ctxt, isPresent(url, &buf1))
	if err != nil && strings.Contains(fmt.Sprint(err), "Uncaught"){
		return
	} else {

		check(err, "Error in Run method of cdp")
	}

	//log.Println("The buf1: \n", string(buf1), "\n\n Len(buf): ", len(buf1))
	if (len(buf1) == 0) || (bytes.EqualFold([]byte("0"), buf1)){

		log.Println("No webyclip-widget-3 on page:\n ", url)
		//log.Println("The buf1: \n", string(buf1))

		// shutdown chrome
		err = c.Shutdown(ctxt)
		check(err, "Error in shutting down chrome")
	
		// wait for chrome to finish
		err = c.Wait()
		check(err, "Error in wait to shutdown chrome")
		
		return

	} else { 
	
	// Exit the code if "webyclip-widget-3" is not present.
		err = c.Run(ctxt, getProductInfo(url, `#webyclip-widget-3`, &buf, &pId, &pUrl, &url))
		check(err, "Error in Run method of cdp")

		// shutdown chrome
		err = c.Shutdown(ctxt)
		check(err, "Error in shutting down chrome")
	
		// wait for chrome to finish
		err = c.Wait()
		check(err, "Error in wait to shutdown chrome")

		//log.Println("\n\nlen(buf):\n\n", len(buf), "\n\nThe buf: ", string(buf))

		pLinks		:= getVideoLinks(buf)
		record		:= fmt.Sprint(pId + "\t" + pUrl + "\t" + pLinks)
		WriteToFile(record)
	}
}

//================================================================================
// getProductInfo extract the required information by using chromedp package
func getProductInfo(urlstr, sel string, res *[]byte, pId, pUrl, url *string) cdp.Tasks {
	return cdp.Tasks{
		cdp.Navigate(urlstr),
		//cdp.Sleep(20 * time.Second),
		cdp.WaitVisible(sel, cdp.ByID),
		cdp.EvaluateAsDevTools("document.getElementById('product-id').value;", pId),
		cdp.EvaluateAsDevTools("document.getElementById('product-url').value;", pUrl),
		cdp.EvaluateAsDevTools("document.getElementById('webyclip-widget-3').contentWindow.document.body.outerHTML;", res),
	}
}

// isPresent checks the existence of webyclip-widget-3 element.
func isPresent(url string, res *[]byte) cdp.Tasks {

	return cdp.Tasks{
		cdp.Navigate(url),
		cdp.Sleep(20 * time.Second),
		cdp.EvaluateAsDevTools("document.getElementById('webyclip-thumbnails').innerHTML", res),

//		cdp.EvaluateAsDevTools("if (document.getElementById('webyclip-thumbnails')) {document.getElementById('webyclip-thumbnails').childElementCount;} else {console.log('0')}", res),
//		cdp.EvaluateAsDevTools("document.getElementById('webyclip-thumbnails').childElementCount", res),

	}

}

//================================================================================
// getVideoLinks returns the Youtube viedo links present in the iframe webyclip-widget-3.
// returns all the links which are comma seperated.
func getVideoLinks(buf []byte) string {

	var videoLinks string

	//Convert byte buffer to String
	innerDoc	:= string(buf[:])
	tmp		:= strings.TrimSpace(innerDoc)

	//Find the videolinks and create one final string
	tmpStr		:= strings.Fields(tmp)
	matchStr	:= "i.ytimg.com/vi/"
	yUrl		:= "https://www.youtube.com/watch?v="

	for _, v := range tmpStr {

		//log.Println("Contains: ", strings.Contains(v, "i.ytimg.com"))
		if strings.Contains(v, matchStr) {

			vv := strings.TrimPrefix(v, "src=\\\"//i.ytimg.com/vi/")
			id := strings.Split(vv, "/")

			//log.Println("https://www.youtube.com/watch?v=" + id[0])
			//log.Println("id: \tlen:\n",len(id), id)

			youtubeLink := yUrl + id[0]
			videoLinks += youtubeLink + ","
		}

	}

	// return the video links
	return videoLinks[:len(videoLinks)-1]
}

//========================================================================================
// outFileDetails logs the crawler and fetcher details.
func outFileDetails() {

	if _, err := os.Stat(productFile); !os.IsNotExist(err) {
		log.Println("The output TSV file location: ", productFile)
	} else {
		log.Println("Required data is not present in any of the URLs of crawled Domain.")
	}
}

// runMemStats calls go routines to print the Memory stats.
func runMemStats(tick time.Duration) {

	var mu sync.Mutex
	go func() {

		c := time.Tick(tick)

		for _ = range c {

			mu.Lock()
			printMemStats()
			mu.Unlock()
		}
	}()
}

// printMemStats logs the Memory stats
func printMemStats() {

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	buf := bytes.NewBuffer(nil)
	
	buf.WriteString(strings.Repeat("=", 72) + "\n") 
	buf.WriteString("Memory Profile:\n")
	buf.WriteString(fmt.Sprintf("\tAlloc: %d Kb\n", mem.Alloc/1024))
	buf.WriteString(fmt.Sprintf("\tTotalAlloc: %d Kb\n", mem.TotalAlloc/1024))
	buf.WriteString(fmt.Sprintf("\tNumGC: %d\n", mem.NumGC))
	buf.WriteString(fmt.Sprintf("\tGoroutines: %d\n", runtime.NumGoroutine()))
	buf.WriteString(strings.Repeat("=", 72))

	log.Println(buf.String())
}
//================================================================================
// WriteToFile writes the required info to the file.
func WriteToFile(record string) {

	domain		:= getDomain()
	filePath	:= pwd() + "/" + domain + "-ProductDetails.csv"

	productFile = filePath
	
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {

		log.Println("File doesn't exists. File will be created with the headers before adding data.")
		// If file does not exists then create it with the header and write records.
		file, err1 := os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err1 != nil {
			log.Println("File Open operation failed.")
			return
		}
		defer file.Close()

		header := fmt.Sprint("Product_ID" + "\t" + "Product_URL" + "\t" + "Youtube_Video_URLs")
		file.WriteString(fmt.Sprintf("%s\n", header))
		file.WriteString(fmt.Sprintf("%s\n", record))
		return

	}
	defer f.Close()

	log.Println("File exists Already. Adding the data for url.")
	f.WriteString(fmt.Sprintf("%s\n", record))
}

// WriteProcessedUrlsToFile writes the processed URLs to the file.
func WriteProcessedUrlsToFile(urls []string) string{

	domain		:= getDomain()
	filePath	:= pwd() + "/" + domain + "-ProcessedURLs.csv"
	urlFile = filePath
	
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	check(err, "Error in file Open operation")
	defer f.Close()

	for _, url := range urls {
		
		f.WriteString(fmt.Sprintf("%s\n", url))
	}
	return filePath
}

//================================================================================
// check checks the error, panics if not nil
func check(err error, str string){

        if err != nil { log.Fatalln(err, str) }
}

// pwd returns the current working directory through which the binary is invoked.
// used to save the csv file.
func pwd() string {
	
	pwd, err := os.Getwd()
	check(err, "Error in getting current workig dir.")
	return pwd

}

// getDomain return only domain name by triming non required contents.
func getDomain() string {

	tmp		:= strings.TrimPrefix(baseurl, "https://www.")
	domain		:= strings.Split(tmp, ".")[0]
	return domain

}

// getOS get the OS information
func getOS() string {

	var path, os string
	os = runtime.GOOS

	switch{

	case os == "linux":
		path = "/usr/bin/google-chrome"

	case os == "windows":
		path = `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`
	}
return path
}
//================================================================================
