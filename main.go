package main

import (
	"container/list"
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
var applog = newlogger()

type queue struct {
	s sync.Mutex
	l *list.List
	m int
}

func (q *queue) enqueue(s string) {
	q.s.Lock()
	defer q.s.Unlock()
	for q.l.Len() > q.m {
		q.dequeue()
	}
	q.l.PushBack(s)
}

func (q *queue) dequeue() string {
	q.s.Lock()
	defer q.s.Unlock()
	v := q.l.Front()
	q.l.Remove(v)
	return v.Value.(string)
}

func (q *queue) loop(f func(string)) {
	q.s.Lock()
	defer q.s.Unlock()
	for e := q.l.Back(); e != nil; e = e.Prev() {
		f(e.Value.(string))
	}
}

type logger struct {
	queue *queue
}

func (l *logger) logf(f string, v ...interface{}) {
	s := fmt.Sprintf(f, v...)
	log.Print(s)
	// l.queue.enqueue(s)
}

func newlogger() *logger {
	al := &logger{
		queue: &queue{
			l: list.New(),
			m: 50,
		},
	}
	return al
}

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
	URL string `xml:"url,attr"`
}

type Episode struct {
	name     string
	subtitle string
	url      string
}

type PodParser interface {
	URLs() []Episode
}

type RssParser string

// URLs extracts media-links from rss
func (rp RssParser) URLs() []Episode {
	res, err := http.Get(string(rp))
	if err != nil {
		applog.logf("%s", err.Error())
		return nil
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		applog.logf("%s", err.Error())
		return nil
	}

	rss := RssFeed{}
	err = xml.Unmarshal(bs, &rss)
	if err != nil {
		applog.logf("%s", err.Error())
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

type Pod struct {
	name       string
	parser     PodParser
	lastUpdate time.Time
	eps        []Episode
}

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
	applog.logf("pods: Updating podcasts")
	for _, pod := range pods {
		applog.logf("pods:\t%s... ", pod.name)
		pod.Update()
		applog.logf("Done!")
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
	http.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		writeflush := func(s string) {
			fmt.Fprintln(w, s)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		io.WriteString(w, strings.Repeat(" ", 1025))
		applog.queue.loop(func(s string) {
			writeflush(s)
		})
	})
	http.ListenAndServe(*port, nil)
}

func index(w http.ResponseWriter, r *http.Request) {
	t, err := template.New("index").Parse(indextemplate)
	if err != nil {
		fmt.Fprint(w, err.Error())
		applog.logf(err.Error())
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
		log.Print(err.Error())
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
