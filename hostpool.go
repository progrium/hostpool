package main

import (
	"strings"
	"os"
	"fmt"
	"log"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"errors"
	"time"
	"io/ioutil"
)

const(
	regionId = "1" // New York 1
	sizeId = "66" // 512MB
)

func jsonGetObj(obj interface{}, key string) interface{} {
	m := obj.(map[string]interface{})
	return m[key]
}

func jsonGetList(obj interface{}, key string) []interface{} {
	return jsonGetObj(obj, key).([]interface{})
}

func jsonGetString(obj interface{}, key string) string {
	o := jsonGetObj(obj, key)
	if o == nil {
		return ""
	}
	return o.(string)
}

func jsonGetFloatAsIntString(obj interface{}, key string) string {
	return strings.Split(strconv.FormatFloat(jsonGetObj(obj, key).(float64), 'f', 6, 64), ".")[0]
}


func doApiCall(url string, params url.Values) (interface{}, error) {
	resp, err := http.Get(url+"?"+params.Encode())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("API request returned "+resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var data interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		return nil, err
	}
	if jsonGetString(data, "status") != "OK" {
		return nil, errors.New("API response status not OK: "+jsonGetString(data, "status"))
	}
	return data, nil
}

func destroyCiHost(id string) error {
	v := url.Values{}
	v.Set("client_id", os.Getenv("CLIENT_ID"))
	v.Set("api_key", os.Getenv("API_KEY"))
	_, err := doApiCall("https://api.digitalocean.com/droplets/"+id+"/destroy", v)
	if err != nil {
		return err
	}
	return nil
}

func createCiHost(semaphore chan struct{}) (string, chan struct{}, error) {
	v := url.Values{}
	v.Set("client_id", os.Getenv("CLIENT_ID"))
	v.Set("api_key", os.Getenv("API_KEY"))
	v.Set("image_id", os.Getenv("IMAGE"))
	if os.Getenv("KEY") != "" {
		v.Set("ssh_key_ids", os.Getenv("KEY"))
	}
	v.Set("name", os.Getenv("NAME") + "." + strconv.Itoa(int(time.Now().Unix())))
	v.Set("size_id", sizeId)
	v.Set("region_id", regionId)
	resp, err := doApiCall("https://api.digitalocean.com/droplets/new", v)
	if err != nil {
		return "", nil, err
	}
	dropletId := jsonGetFloatAsIntString(jsonGetObj(resp, "droplet"), "id")
	killSignal := make(chan struct{})
	go func() {
		<- killSignal
		log.Println("Destroying "+dropletId)
		err := destroyCiHost(dropletId)
		if err != nil {
			log.Println(err)
		}
		<- semaphore
	}()
	eventId := jsonGetFloatAsIntString(jsonGetObj(resp, "droplet"), "event_id")
	v = url.Values{}
	v.Set("client_id", os.Getenv("CLIENT_ID"))
	v.Set("api_key", os.Getenv("API_KEY"))
	done := false
	time.Sleep(5 * time.Second)
	for !done {
		time.Sleep(5 * time.Second)
		resp, err := doApiCall("https://api.digitalocean.com/events/"+eventId, v)
		if err != nil {
			break
		}
		done = (jsonGetString(jsonGetObj(resp, "event"), "percentage") == "100")
	}
	resp, err = doApiCall("https://api.digitalocean.com/droplets/"+dropletId, v)
	if err != nil {
		return "", killSignal, err
	}
	droplet := jsonGetObj(resp, "droplet")
	if jsonGetString(droplet, "status") != "active" {
		return "", killSignal, errors.New("Droplet not active: "+jsonGetString(droplet, "name"))
	}
	ip := jsonGetString(droplet, "ip_address")
	return ip, killSignal, nil
}

func clearCiHosts() error {
	v := url.Values{}
	v.Set("client_id", os.Getenv("CLIENT_ID"))
	v.Set("api_key", os.Getenv("API_KEY"))
	resp, err := doApiCall("https://api.digitalocean.com/droplets", v)
	if err != nil {
		return err
	}
	for _, droplet := range jsonGetList(resp, "droplets") {
		name := jsonGetString(droplet, "name")
		if strings.HasPrefix(name, os.Getenv("NAME")+".") {
			log.Println("Destroying "+name)
			err := destroyCiHost(jsonGetFloatAsIntString(droplet, "id"))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	config := strings.Split("PORT CLIENT_ID API_KEY IMAGE CONCURRENCY NAME TIMEOUT", " ")
	for _, key := range config {
		if os.Getenv(key) == "" {
			fmt.Println("All configuration is required:")
			fmt.Println(strings.Join(config, "\n"))
			os.Exit(1)
		}
	}
	port := os.Getenv("PORT")
	concurrency, err := strconv.Atoi(os.Getenv("CONCURRENCY"))
	if err != nil {
		log.Fatal(err)
	}
	timeout, err := strconv.Atoi(os.Getenv("TIMEOUT"))
	if err != nil {
		log.Fatal(err)
	}

	err = clearCiHosts()
	if err != nil {
		time.Sleep(20 * time.Second) // adds delay to retry if in reboot loop
		log.Fatal(err)
	}
	
	semaphore := make(chan struct{}, concurrency)

	http.HandleFunc("/hosts", func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI == "/favicon.ico" {
			return
		}
		switch r.Method {
		case "GET":
			disconnected := false
			go func() { 
				disconnected = <-w.(http.CloseNotifier).CloseNotify() 
			}()
			go func() {
				for {
					time.Sleep(15 * time.Second)
					if disconnected {
						break
					}
					w.Write([]byte("\n"))
				}
			}()
			log.Printf("Request to create host. Slots: %v/%v\n", len(semaphore), cap(semaphore))
			semaphore <- struct{}{}
			if disconnected {
				<- semaphore
				log.Println("Client disconnected while in queue")
				return
			}
			log.Println("Proceeding with host creation")
			ip, killSignal, err := createCiHost(semaphore)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				log.Println(err.Error())
				if killSignal != nil {
					close(killSignal)
				}
				return
			}
			if disconnected {
				log.Println("Client disconnected during host creation")
				close(killSignal)
				return
			}
			log.Println("Host created: "+ip)
			disconnected = true // stop heartbeats
			w.Write([]byte(ip))
			go func() {
				time.Sleep(time.Duration(timeout) * time.Minute)
				close(killSignal)
			}()
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	log.Println("Listening on port "+port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
