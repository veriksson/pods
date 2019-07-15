package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var port = flag.String("port", ":6363", "port to listen to :XXXX")

// RssFeed is the root of the feed
type RssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel RssChannel `xml:"channel"`
}

// RssChannel is a channel
type RssChannel struct {
	Title string    `xml:"title"`
	Items []RssItem `xml:"item"`
}

// RssItem represents an individual item in the channel
type RssItem struct {
	Title     string       `xml:"title"`
	Enclosure RssEnclosure `xml:"enclosure"`
	Subtitle  string       `xml:"itunes:subtitle"`
}

// RssEnclosure is the metadata + url of the item
type RssEnclosure struct {
	URL string `xml:"url,attr"`
}

// Episode is used in the template
type Episode struct {
	name     string
	subtitle string
	url      string
}

type parser interface {
	URLs() []Episode
}

// RssParser implements the parser interface and the  string is the url for the feed
type RssParser string

// URLs extracts media-links from rss
func (rp RssParser) URLs() []Episode {
	res, err := http.Get(string(rp))
	if err != nil {
		log.Printf("%s", err.Error())
		return nil
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Printf("%s", err.Error())
		return nil
	}

	rss := RssFeed{}
	err = xml.Unmarshal(bs, &rss)
	if err != nil {
		log.Printf("%s", err.Error())
		return nil
	}

	l := len(rss.Channel.Items)
	if l > 10 {
		l = 10
	}
	eps := make([]Episode, l)
	for i := 0; i < len(eps); i++ {
		eps[i] = Episode{rss.Channel.Items[i].Title,
			rss.Channel.Items[i].Subtitle,
			rss.Channel.Items[i].Enclosure.URL}
	}
	return eps
}

// Pod keeps track and updates the feed
type Pod struct {
	name       string
	parser     parser
	lastUpdate time.Time
	eps        []Episode
}

// Update the feed items
func (p *Pod) Update() {
	eps := p.parser.URLs()

	p.lastUpdate = time.Now()
	sort.Slice(eps, func(i, j int) bool {
		return eps[i].name > eps[j].name
	})
	p.eps = eps
}

var m sync.Mutex
var pods = make(map[string]*Pod)

func update() {
	m.Lock()
	log.Print("pods: Updating podcasts")
	for _, pod := range pods {
		log.Printf("pods:\t%s... ", pod.name)
		pod.Update()
		log.Print("Done!")
	}
	m.Unlock()
}

func sched() {
	update()
	c := time.Tick(1 * time.Hour)
	for range c {
		update()
	}
}

func main() {
	flag.Parse()
	podcast := &Pod{
		name:       "Filip & Fredrik",
		lastUpdate: time.Now(),
		parser:     RssParser("https://feed.pod.space/filipandfredrik"),
	}

	pods["filip & fredrik"] = podcast

	aosPod := &Pod{
		name:       "Alex & Sigge",
		lastUpdate: time.Now(),
		parser:     RssParser("http://alexosigge.libsyn.com/rss"),
	}

	pods["alex & sigge"] = aosPod

	kodsnackPod := &Pod{
		parser:     RssParser("https://kodsnack.libsyn.com/rss"),
		lastUpdate: time.Now(),
		name:       "Kodsnack",
	}

	pods["kodsnack"] = kodsnackPod

	gotimePod := &Pod{
		name:       "Go Time",
		lastUpdate: time.Now(),
		parser:     RssParser("https://changelog.com/gotime/feed"),
	}

	pods["go time"] = gotimePod

	seradioPod := &Pod{
		name:       "SE Radio",
		lastUpdate: time.Now(),
		parser:     RssParser("https://www.se-radio.net/feed/podcast/"),
	}

	pods["se radio"] = seradioPod

	bikeshedFM := &Pod{
		name:       "The Bike Shed",
		lastUpdate: time.Now(),
		parser:     RssParser("https://rss.simplecast.com/podcasts/282/rss"),
	}

	pods["bikeshed"] = bikeshedFM

	go sched()
	http.HandleFunc("/", index)
	http.HandleFunc("/forceupdate", func(w http.ResponseWriter, r *http.Request) {
		writeflush := func(s string) {
			fmt.Fprint(w, s)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		io.WriteString(w, strings.Repeat(" ", 1025))
		writeflush("Starting update... ")
		update()
		writeflush("Done")
	})
	http.ListenAndServe(*port, nil)
}

func index(w http.ResponseWriter, r *http.Request) {
	t, err := template.New("index").Parse(indextemplate)
	if err != nil {
		fmt.Fprint(w, err.Error())
		log.Print(err.Error())
		return
	}
	var data []TemplatePod

	m.Lock()
	for name, pod := range pods {
		tp := TemplatePod{Name: name,
			LastUpdate: pod.lastUpdate.Format("2006-01-02 15:04"),
			Episodes:   make([]TemplateEpisode, len(pod.eps))}
		for i := range pod.eps {
			tp.Episodes[i] = TemplateEpisode{Title: pod.eps[i].name, URL: pod.eps[i].url}
		}
		data = append(data, tp)
	}
	m.Unlock()
	err = t.Execute(w, data)
	if err != nil {
		log.Print(err.Error())
	}
}

// TemplateEpisode is for the html template
type TemplateEpisode struct {
	Title string
	URL   string
}

// TemplatePod is for the html template
type TemplatePod struct {
	Name       string
	LastUpdate string
	Episodes   []TemplateEpisode
}

var indextemplate = `
	<!DOCTYPE html>
	<html>
		<head>
			<meta charset="utf-8" />
			<title>Pods</title>
			<style type="text/css">
				* {
					font-family: Go Mono, Terminal, Consolas, Lucida Console;
				}
				body {
					display: flex;
					flex-wrap: wrap;
					margin: 1em auto;
					max-width: 1200px;
					color: #444;
					font-size: 18px;
					line-height: 1.6;
				} 
			</style>
		</head>
		<body>
		{{ range . }}
			<div style="width: 600px">
				<h3><strong>{{ .Name }}</strong></h3>
				<i>{{ .LastUpdate }}</i><br />
				<ul>
				{{ range .Episodes }}
					<li><a href="{{ .URL }}" target="_blank">{{ .Title }}</a></li>
				{{ end }}	
				</ul>
			</div>
		{{ end }}
		
	 </body>
	</html>`
