package main 

import "fmt"
import "github.com/PuerkitoBio/goquery"
import "encoding/json"
import "strings"
import "net/http"
import "io/ioutil"
import "regexp" 
import "sync"

type Episode struct {
	name	string
	url	string
}

type PodParser interface {
	FindPodcastURLs(doc *goquery.Document) []Episode
}

type FFPod string
func (p FFPod) FindPodcastURLs(doc *goquery.Document) []Episode { 
	js := doc.Find("script").Eq(0).Text()
	i := strings.Index(js, "{\"G")
	j := strings.Index(js, "};") + 1
	jsonData := []byte(js[i:j])
	var m map[string]interface{}
	err := json.Unmarshal(jsonData, &m)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	if casts, ok := m["GetAcastsByChannel#filipandfredrik#0"]; ok {
		var wg sync.WaitGroup
		var mux sync.Mutex
		var episodes []Episode
		for _, cast := range (casts).([]interface{}) {
			wg.Add(1)
			title := cast.(map[string]interface{})["name"].(string)
			url := cast.(map[string]interface{})["url"].(string)
			docUri := strings.Replace(doc.Url.String(), "www.", "embed.", 1)
			go func(title, url string) {
				mp3 := p.parseSpecificPage(url)
				mux.Lock()
				episodes = append(episodes, Episode { title, mp3 })
				mux.Unlock()
				wg.Done()
			}(title, docUri + url)
		}
		wg.Wait()
		fmt.Println("Done waiting!")
		return episodes
	}
	return nil
}

func (p FFPod) parseSpecificPage(url string) string {
	r, _ := regexp.Compile("https://.*\\.mp3") // this will either work or not. don't check error
	
	page, err := http.Get(url)
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}	
	defer page.Body.Close()

	body, err := ioutil.ReadAll(page.Body)
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}

	s := string(body[:])
	return r.FindString(s)
}

type Pod struct {
	url	string
	latest	string
	parser	PodParser
}

func (p Pod) getPodcastArchive() (*goquery.Document, error) {
	return goquery.NewDocument(p.url)
}

func (p Pod) Do() {
	d, err := p.getPodcastArchive()
	if err != nil {
		fmt.Println(err.Error())
		return	
	}

	eps := p.parser.FindPodcastURLs(d)
	for _, ep := range eps {
		fmt.Println(ep.url)
	}
}

func main() {
	parser := FFPod("Filip & Fredrik")
	podcast := Pod {
		url: "https://www.acast.com/filipandfredrik/",
		latest: "none",
		parser: parser,
	}
	podcast.Do()
}
