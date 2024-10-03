package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	sitemapURL         = "https://docs.aws.amazon.com/sitemap_index.xml"
	rateLimitDelay     = 2 * time.Second // Delay between each request to prevent rate limiting
	maxBackoffAttempts = 5               // Maximum number of backoff attempts before giving up
	sleepDuration      = 3 * time.Second // Time to sleep on rate limit detection or failure
)

var (
	rateLimitEnabled bool // Global flag for rate limiting
	userAgents       = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:102.0) Gecko/20100101 Firefox/102.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.5 Safari/605.1.15",
	}
)

// List of SDKs to exclude
var sdkExclusions = []string{
	"AWSJavaSDK",
	"AWSJavaScriptSDK",
	"CDI-SDK",
	"aws-sdk-php",
	"chime-sdk",
	"database-encryption-sdk",
	"embedded-csdk",
	"encryption-sdk",
	"pythonsdk",
	"sdk-for-android",
	"sdk-for-cpp",
	"sdk-for-go",
	"sdk-for-ios",
	"sdk-for-java",
	"sdk-for-javascript",
	"sdk-for-kotlin",
	"sdk-for-net",
	"sdk-for-php",
	"sdk-for-php1",
	"sdk-for-ruby",
	"sdk-for-rust",
	"sdk-for-sapabap",
	"sdk-for-swift",
	"sdk-for-unity",
	"sdkfornet",
	"sdkfornet1",
	"sdkref",
	"xray-sdk-for-java",
}

var excludeRegex *regexp.Regexp

func init() {
	// Prepare the list of SDKs for regex
	escapedSDKs := make([]string, len(sdkExclusions))
	for i, sdk := range sdkExclusions {
		escapedSDKs[i] = regexp.QuoteMeta(sdk)
	}

	// Build the regex pattern
	pattern := fmt.Sprintf(`https://docs\.aws\.amazon\.com/(?:[a-z]{2}_[a-z]{2}|cdk|%s)/`, strings.Join(escapedSDKs, "|"))

	excludeRegex = regexp.MustCompile(pattern)
}

// SitemapIndex represents the structure of the sitemap index XML.
type SitemapIndex struct {
	XMLName  xml.Name     `xml:"sitemapindex"`
	Sitemaps []SitemapLoc `xml:"sitemap"`
}

// SitemapLoc represents the location of each sitemap in a sitemap index.
type SitemapLoc struct {
	Loc string `xml:"loc"`
}

// URLSet represents the structure of a URL set XML (the list of URLs in a sitemap).
type URLSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []URLLoc `xml:"url"`
}

// URLLoc represents each URL in a URL set.
type URLLoc struct {
	Loc string `xml:"loc"`
}

func main() {
	// Command-line flags
	test := flag.Int("test", 0, "Specify the number of documents to download for testing")
	logFile := flag.String("logfile", "", "Specify a file to write debug logs to")
	maxWorkers := flag.Int("workers", 10, "Number of concurrent workers to download files")
	flag.BoolVar(&rateLimitEnabled, "rate-limit", false, "Enable rate limiting to avoid 403 errors")
	flag.Parse()

	// Set up logging
	if *logFile != "" {
		logFileHandle, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
		defer logFileHandle.Close()
		log.SetOutput(logFileHandle)
		log.Println("Debug mode enabled - logs written to file.")
	} else {
		log.SetOutput(os.Stdout)
	}

	log.Println("Starting AWS documentation scraping")

	urlChannel := make(chan string)
	var wg sync.WaitGroup

	// Start workers to download files concurrently
	for i := 0; i < *maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for url := range urlChannel {
				downloadAndSaveAsHTML(url)
				// If rate limiting is enabled, sleep between requests
				if rateLimitEnabled {
					time.Sleep(rateLimitDelay) // Delay between requests if rate limiting is enabled
				}
			}
		}()
	}

	// Fetch and parse the sitemap concurrently
	go func() {
		defer close(urlChannel) // Close the channel when done
		err := fetchAndParseSitemap(sitemapURL, *test, urlChannel)
		if err != nil {
			log.Fatalf("Error fetching sitemap: %v", err)
		}
	}()

	// Wait for all downloads to finish
	wg.Wait()
	log.Println("Scraping finished")
}

// fetchAndParseSitemap fetches and parses a sitemap, handling both sitemap indexes and URL sets.
func fetchAndParseSitemap(sitemapURL string, maxDocs int, urlChannel chan<- string) error {
	// Replace http with https
	sitemapURL = strings.Replace(sitemapURL, "http://", "https://", 1)

	parsedURL, err := url.Parse(sitemapURL)
	if err != nil {
		log.Printf("Error parsing sitemap URL %s: %v", sitemapURL, err)
		return err
	}

	// Ensure the sitemap URL is under docs.aws.amazon.com
	if parsedURL.Host != "docs.aws.amazon.com" {
		log.Printf("Skipping sitemap from other domain: %s", sitemapURL)
		return nil
	}

	// Exclude if matches excludeRegex
	if excludeRegex.MatchString(sitemapURL) {
		log.Printf("Skipping excluded sitemap: %s", sitemapURL)
		return nil
	}

	log.Printf("Fetching sitemap: %s", sitemapURL)
	resp, err := fetchWithRateLimitHandling(sitemapURL)
	if err != nil {
		log.Printf("Error fetching sitemap %s: %v", sitemapURL, err)
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// First try parsing the body as a SitemapIndex
	var sitemapIndex SitemapIndex
	err = xml.Unmarshal(body, &sitemapIndex)
	if err == nil && len(sitemapIndex.Sitemaps) > 0 {
		log.Printf("Parsed sitemap as a SitemapIndex: %s", sitemapURL)
		// Recursively fetch child sitemaps
		for _, sitemap := range sitemapIndex.Sitemaps {
			err := fetchAndParseSitemap(sitemap.Loc, maxDocs, urlChannel)
			if err != nil {
				log.Printf("Error fetching child sitemap: %v", err)
			}
		}
		return nil
	}

	// If it's not a SitemapIndex, try parsing it as a URLSet
	var urlSet URLSet
	err = xml.Unmarshal(body, &urlSet)
	if err == nil && len(urlSet.URLs) > 0 {
		log.Printf("Parsed sitemap as a URLSet: %s", sitemapURL)
		for i, urlEntry := range urlSet.URLs {
			// Replace http with https
			urlEntry.Loc = strings.Replace(urlEntry.Loc, "http://", "https://", 1)

			parsedURL, err := url.Parse(urlEntry.Loc)
			if err != nil {
				log.Printf("Error parsing URL %s: %v", urlEntry.Loc, err)
				continue
			}

			// Ensure the URL is under docs.aws.amazon.com
			if parsedURL.Host != "docs.aws.amazon.com" {
				log.Printf("Skipping URL from other domain: %s", urlEntry.Loc)
				continue
			}

			// Check if the URL matches the exclusion pattern
			if excludeRegex.MatchString(urlEntry.Loc) {
				log.Printf("Skipping excluded URL: %s", urlEntry.Loc)
				continue
			}

			urlChannel <- urlEntry.Loc
			log.Printf("Queued URL for download: %s", urlEntry.Loc)

			if maxDocs > 0 && i+1 >= maxDocs {
				break
			}
		}
		return nil
	}

	// If parsing fails, log an error
	log.Printf("Error parsing sitemap: unable to determine type for URL %s\n", sitemapURL)
	return fmt.Errorf("unable to parse sitemap at %s", sitemapURL)
}

// fetchWithRateLimitHandling fetches the document from the given URL and handles 403 rate limiting or connection errors.
func fetchWithRateLimitHandling(url string) (*http.Response, error) {
	maxRetries := 5
	for retries := 0; retries < maxRetries; retries++ {
		// Randomly select a user agent from the list
		userAgent := userAgents[rand.Intn(len(userAgents))]

		// Create a new HTTP request
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		// Set the user agent header
		req.Header.Set("User-Agent", userAgent)

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			// Check if it's a temporary network error
			if nerr, ok := err.(net.Error); ok && nerr.Temporary() {
				log.Printf("Temporary error fetching URL: %v, retrying in %s", err, sleepDuration)
				time.Sleep(sleepDuration)
				continue
			} else {
				// Non-recoverable error, log and exit
				log.Printf("Error fetching URL: %v", err)
				return nil, err
			}
		}

		// If successful response, return it
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		// Handle rate limiting (403 Forbidden)
		if resp.StatusCode == http.StatusForbidden {
			log.Printf("Received 403 Forbidden (rate limit), pausing for %s before retrying...", sleepDuration)
			time.Sleep(sleepDuration)
			resp.Body.Close()
			continue
		}

		// Handle other unexpected status codes
		log.Printf("Unexpected status code: %d for URL %s", resp.StatusCode, url)
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d for URL %s", resp.StatusCode, url)
	}
	return nil, fmt.Errorf("max retries exceeded for URL %s", url)
}

func downloadAndSaveAsHTML(url string) {
	// Get the current date
	now := time.Now()
	datePath := filepath.Join(
		// Adding year, month, and day to the directory path
		"aws_html",
		now.Format("2006"), // Year
		now.Format("01"),   // Month
		now.Format("02"),   // Day
	)

	// Remove the protocol part (https://) and construct the URL-based directory structure
	trimmedURL := strings.TrimPrefix(url, "https://")

	// Determine the directory path and file name
	var dirPath, htmlFilePath string
	if strings.HasSuffix(trimmedURL, "/") {
		dirPath = filepath.Join(datePath, trimmedURL)
		htmlFilePath = filepath.Join(dirPath, "index.html")
	} else {
		dirPath = filepath.Join(datePath, filepath.Dir(trimmedURL))
		fileName := filepath.Base(trimmedURL)
		htmlFilePath = filepath.Join(dirPath, fileName)
	}

	// Create the directory structure
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		log.Printf("Error creating directory: %v", err)
		return
	}

	// Fetch document
	log.Printf("Downloading document: %s\n", url)
	resp, err := fetchWithRateLimitHandling(url)
	if err != nil {
		log.Printf("Error downloading document: %v", err)
		return
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return
	}

	// Write to file
	err = os.WriteFile(htmlFilePath, bodyBytes, 0644)
	if err != nil {
		log.Printf("Error writing HTML file: %v", err)
		return
	}

	log.Printf("Successfully saved HTML file: %s\n", htmlFilePath)
}
