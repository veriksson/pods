package main 

import "fmt"
import "github.com/PuerkitoBio/goquery"
import "encoding/json"
import "strings"
import "net/http"
import "io/ioutil"
import "regexp" 
import "sync"
import "time"
import "html/template"
import "sort"
import "flag"
import "encoding/xml"

var Port = flag.String("port", ":6363", "port to listen to :XXXX")

type RssFeed struct {
	XMLName	xml.Name	`xml:"rss"`
	Channel	RssChannel	`xml:"channel"`
}

type RssChannel struct {
	Title	string		`xml:"title"`
	Items	[]RssItem	`xml:"item"`
}

type RssItem struct {
	Title		string		`xml:"title"`
	Enclosure	RssEnclosure	`xml:"enclosure"`
}

type RssEnclosure struct {
	Url	string	`xml:"url,attr"`
}

type byEpisodeName []Episode

func (b byEpisodeName) Len() int { return len(b) }
func (b byEpisodeName) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byEpisodeName) Less(i, j int) bool { return b[i].name < b[j].name }

type Episode struct {
	name	string
	url	string
}

type PodParser interface {
	FindPodcastURLs(url string) []Episode
}

type RssPod string
func (p RssPod) FindPodcastURLs(url string) []Episode {
	res, err := http.Get(url)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}	
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}

	rss := RssFeed{}
	err = xml.Unmarshal(bs, &rss)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}

	l := len(rss.Channel.Items)
	if l > 10 {
		l = 10
	}
	eps := make([]Episode, l)
	for i := 0; i < len(eps); i++ {
		eps[i] = Episode { rss.Channel.Items[i].Title, rss.Channel.Items[i].Enclosure.Url } 
	}
	return eps
}

type AcastPod string
func (p AcastPod) FindPodcastURLs(url string) []Episode { 
	doc, err := goquery.NewDocument(url)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	js := doc.Find("script").Eq(0).Text()
	i := strings.Index(js, "{\"G")
	j := strings.Index(js, "};") + 1
	jsonData := []byte(js[i:j])
	var m map[string]interface{}
	err = json.Unmarshal(jsonData, &m)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	//TODO(v): fix json map to be more general
	if casts, ok := m["GetAcastsByChannel#filipandfredrik#0"]; ok {
		var wg sync.WaitGroup
		var episodes []Episode
		eps := make(chan Episode)
		for _, cast := range (casts).([]interface{}) {
			wg.Add(1)
			title := cast.(map[string]interface{})["name"].(string)
			url := cast.(map[string]interface{})["url"].(string)
			docUri := strings.Replace(doc.Url.String(), "www.", "embed.", 1)
			go func(title, url string) {
				mp3 := p.parseSpecificPage(url)
				eps <- Episode { title, mp3 }
				wg.Done()
			}(title, docUri + url)
		}
		go func() {
			for ep := range eps {
				episodes = append(episodes, ep)
			}
		}()	

		wg.Wait()
		close(eps)
		return episodes
	}
	return nil
}

func (p AcastPod) parseSpecificPage(url string) string {
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
	url		string
	parser		PodParser
	lastUpdate 	time.Time
	eps 		[]Episode
}

func (p *Pod) getPodcastArchive() (*goquery.Document, error) {
	return goquery.NewDocument(p.url)
}

func (p *Pod) Do() {
	eps := p.parser.FindPodcastURLs(p.url)

	p.lastUpdate = time.Now()
	sort.Sort(sort.Reverse(byEpisodeName(eps)))
	p.eps = eps
}

var m sync.Mutex
var pods = make(map[string]*Pod)

func update() {
	m.Lock()
	fmt.Println("Updating podcasts")
	for name, pod := range pods {
		fmt.Printf("* %s... ", name)
		pod.Do()
		fmt.Println("Done!")
	}
	m.Unlock()
}

func sched() {	
	update()
	c:= time.Tick(1 * time.Hour)
	for _ = range c {
		update()
	}
}



func main() {
	flag.Parse()
	parser := AcastPod("Filip & Fredrik")
	podcast := &Pod {
		url: "https://www.acast.com/filipandfredrik/",
		lastUpdate: time.Now(),
		parser: parser,
	}
	pods["filip & fredrik"] = podcast
	
	aosParser := RssPod("Alex & Sigge")
	aosPod := &Pod {
		url: "http://alexosigge.libsyn.com/rss",
		lastUpdate: time.Now(),
		parser: aosParser,
	}

	pods["alex & sigge"]= aosPod

	ftmParser := RssPod("F This Movie!")
	ftmPod := &Pod {
		url: "http://feeds.feedburner.com/fthismovie?format=xml",
		lastUpdate: time.Now(),
		parser: ftmParser,
	}

	pods["f this movie!"] = ftmPod

	go sched()
	http.HandleFunc("/", IndexHandler)
	http.HandleFunc("/forceupdate", func(w http.ResponseWriter, r *http.Request) {
		writeflush := func (s string) {
			fmt.Fprint(w, s)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		writeflush("Starting update... ")
		update()
		writeflush("Done")
	})
	http.ListenAndServe(*Port, nil)
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	t, err := template.New("index").Parse(indextemplate)
	if err != nil {
		fmt.Fprint(w, err.Error())
		fmt.Println(err.Error())
		return 
	}
	var data []TemplatePod

	m.Lock()
	for name, pod := range pods {
		tpod := TemplatePod { Name: name, LastUpdate: pod.lastUpdate, Episodes: make([]TemplateEpisode, len(pod.eps)) }
		for i := range pod.eps {
			tpod.Episodes[i] = TemplateEpisode { Title: pod.eps[i].name, URL: pod.eps[i].url }
		}
		data = append(data, tpod)
	}
	m.Unlock()
	err =	t.Execute(w, data)
	if err != nil {
		fmt.Println(err.Error())
	}
}

type TemplateEpisode struct {
	Title	string
	URL	string
}

type TemplatePod struct {
	Name		string
	LastUpdate	time.Time
	Episodes	[]TemplateEpisode
}

var indextemplate string = `
	<!DOCTYPE html>
	<html>
		<head>
			<meta charset="utf-8" />
			<title>Pods</title>
			<style type="text/css">
				* {
					font-family: Terminal, Consolas, Lucida Console;
				}
			</style>
		</head>
		<body>
		{{ range . }}
			<h3><strong>{{ .Name }}</strong></h3>
			<i>{{ .LastUpdate }}</i><br />
			<ul>
			{{ range .Episodes }}
				<li><a href="{{ .URL }}" target="_blank">{{ .Title }}</a></li>
			{{ end }}
			</ul>
		{{ end }}
		<br />
		</body>
	</html>`


			
