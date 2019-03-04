package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var port = flag.String("port", ":6363", "port to listen to :XXXX")
var p = fmt.Printf
var fp = fmt.Fprint

type RssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel RssChannel `xml:"channel"`
}

type RssChannel struct {
	Title string    `xml:"title"`
	Items []RssItem `xml:"item"`
}

type RssItem struct {
	Title     string       `xml:"title"`
	Enclosure RssEnclosure `xml:"enclosure"`
	Subtitle  string       `xml:"itunes:subtitle"`
}

type RssEnclosure struct {
	Url string `xml:"url,attr"`
}

type byEpisodeName []Episode

func (b byEpisodeName) Len() int           { return len(b) }
func (b byEpisodeName) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byEpisodeName) Less(i, j int) bool { return b[i].name < b[j].name }

type Episode struct {
	name     string
	subtitle string
	url      string
}

type PodParser interface {
	FindPodcastURLs(url string) []Episode
}

type RssPod string

// FindPodcastURLs extracts media-links from rss
func (rp RssPod) FindPodcastURLs(url string) []Episode {
	res, err := http.Get(url)
	if err != nil {
		p("%s\n", err.Error())
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
		p(err.Error())
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
			rss.Channel.Items[i].Enclosure.Url}
	}
	return eps
}

type Pod struct {
	url        string
	parser     PodParser
	lastUpdate time.Time
	eps        []Episode
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
	p("pods: Updating podcasts\n")
	for name, pod := range pods {
		p("pods:\t%s... ", name)
		pod.Do()
		p("Done!\n")
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
	parser := RssPod("Filip & Fredrik")
	podcast := &Pod{
		url:        "https://rss.acast.com/filipandfredrik",
		lastUpdate: time.Now(),
		parser:     parser,
	}
	pods["filip & fredrik"] = podcast

	aosParser := RssPod("Alex & Sigge")
	aosPod := &Pod{
		url:        "http://alexosigge.libsyn.com/rss",
		lastUpdate: time.Now(),
		parser:     aosParser,
	}

	pods["alex & sigge"] = aosPod

	ftmParser := RssPod("F This Movie!")
	ftmPod := &Pod{
		url:        "http://feeds.feedburner.com/fthismovie?format=xml",
		lastUpdate: time.Now(),
		parser:     ftmParser,
	}

	pods["f this movie!"] = ftmPod

	gotimeParser := RssPod("Go Time")
	gotimePod := &Pod{
		url:        "https://changelog.com/gotime/feed",
		lastUpdate: time.Now(),
		parser:     gotimeParser,
	}

	pods["go time"] = gotimePod

	kodsnackParser := RssPod("Kodsnack")
	kodsnackPod := &Pod{
		url:        "https://kodsnack.libsyn.com/rss",
		lastUpdate: time.Now(),
		parser:     kodsnackParser,
	}

	pods["kodsnack"] = kodsnackPod

	go sched()
	http.HandleFunc("/", index)
	http.HandleFunc("/forceupdate", func(w http.ResponseWriter, r *http.Request) {
		writeflush := func(s string) {
			fp(w, s)
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
		fp(w, err.Error())
		p(err.Error())
		return
	}
	var data []TemplatePod

	m.Lock()
	for name, pod := range pods {
		tpod := TemplatePod{Name: name,
			LastUpdate: pod.lastUpdate.Format("2006-01-02 15:04"),
			Episodes:   make([]TemplateEpisode, len(pod.eps))}
		for i := range pod.eps {
			tpod.Episodes[i] = TemplateEpisode{Title: pod.eps[i].name, URL: pod.eps[i].url}
		}
		data = append(data, tpod)
	}
	m.Unlock()
	err = t.Execute(w, data)
	if err != nil {
		p(err.Error())
	}
}

type TemplateEpisode struct {
	Title string
	URL   string
}

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
