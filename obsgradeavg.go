package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/PuerkitoBio/goquery"
	"github.com/djimenez/iconv-go"
	"golang.org/x/crypto/ssh/terminal"
)

const obsURL = "https://obs.fbi.h-da.de/obs/"

type module struct {
	name string
	avg  float32
	cp   float32
}

func main() {
	username := flag.String("username", "", "Your OBS username")
	password := flag.String("password", "", "Your OBS password")
	flag.Parse()

	if *username == "" {
		fmt.Print("Please enter your OBS username: ")
		reader := bufio.NewReader(os.Stdin)
		var err error
		*username, err = reader.ReadString('\n')
		if err != nil {
			fmt.Print("Failed to read username", err)
			os.Exit(1)
		}
		*username = strings.TrimSuffix(*username, "\n")
	}
	if *password == "" {
		fmt.Print("Please enter your OBS password: ")
		passwordBytes, err := terminal.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fmt.Print("Failed to read password", err)
			os.Exit(1)
		}
		*password = string(passwordBytes)
	}

	fmt.Print("Logging in... ")

	cookieJar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: cookieJar,
	}

	if !login(client, *username, *password) {
		fmt.Println("failed")
		return
	}
	fmt.Println("success")

	fmt.Println("Gathering average grades per module")
	modules, err := parseModules(client)
	exitOnError(err)

	cpSum := float32(0)
	gradeSum := float32(0)
	for _, m := range modules {
		cpSum += m.cp
		gradeSum += float32(m.cp) * m.avg
	}
	fmt.Printf("The total average is %.2f at currently %.1f cp.\n", gradeSum/float32(cpSum), cpSum)
}

func login(client *http.Client, username string, password string) bool {
	res, err := client.Get(obsURL)
	exitOnError(err)
	doc, err := goquery.NewDocumentFromReader(res.Body)
	loginTan, _ := doc.Find("input[name=\"LoginTAN\"]").Attr("value")

	values := make(url.Values, 3)
	values.Add("username", username)
	values.Add("password", password)
	values.Add("LoginTAN", loginTan)
	res, err = client.PostForm(obsURL+"login.php?action=login", values)
	buf := new(bytes.Buffer)
	buf.ReadFrom(res.Body)

	return len(buf.String()) > 20000
}

func parseModules(client *http.Client) (grades []module, err error) {
	grades = make([]module, 0)

	res, err := client.Get(obsURL + "index.php?action=noten")
	if err != nil {
		return
	}
	//convert charset to utf-8
	utfBody, err := iconv.NewReader(res.Body, "windows-1252", "utf-8")
	if err != nil {
		return
	}
	doc, err := goquery.NewDocumentFromReader(utfBody)
	if err != nil {
		return
	}
	rows := doc.Find("#formAlleNoten tbody tr")
	rows.Each(func(i int, row *goquery.Selection) {
		if i == 0 ||
			i == rows.Length()-1 ||
			row.Children().Size() != 9 ||
			row.Children().Eq(7).Children().Size() == 0 {
			return
		}

		grade, _ := strconv.ParseFloat(row.Children().Eq(6).Text(), 32)
		if grade == 5.0 {
			return
		}

		mu, _ := row.Children().Eq(4).Children().Eq(0).Attr("href")
		cp := getCPForModule(client, mu)
		if cp == 0 {
			return
		}

		moduleName := row.Children().Eq(3).Text()
		if strings.Contains(moduleName, "(PVL)") {
			return
		}
		fmt.Printf("Module '%v' (%.1f cp)...", moduleName, cp)
		statID, _ := row.Children().Eq(7).Children().Attr("href")
		statID = strings.TrimSuffix(strings.TrimPrefix(statID, "javascript:Statistik('"), "')")
		avg := calculateAvgGrade(client, statID)
		if avg == -1 {
			fmt.Println(" not enough module members.")
			return
		}
		fmt.Printf(" %.2f\n", avg)
		m := module{
			name: moduleName,
			avg:  avg,
			cp:   cp,
		}
		grades = append(grades, m)
	})
	return
}

func getCPForModule(client *http.Client, moduleLink string) float32 {
	res, err := client.Get(moduleLink)
	exitOnError(err)
	doc, err := goquery.NewDocumentFromReader(res.Body)
	exitOnError(err)
	cpt := doc.Find("#content table tbody").Children().Eq(6).Children().Eq(1).Text()
	cp, err := strconv.ParseFloat(strings.Replace(cpt, ",", ".", 1), 32)
	exitOnError(err)
	return float32(cp)
}

func calculateAvgGrade(client *http.Client, statID string) float32 {
	res, err := client.Get(obsURL + "index.php?action=Notenstatistik&statpar=" + statID)
	exitOnError(err)
	doc, err := goquery.NewDocumentFromReader(res.Body)
	exitOnError(err)
	gradeRows := doc.Find("span > span")
	if gradeRows.Length() == 0 {
		return -1
	}
	gradesCnt := 0
	gradesSum := float32(0.0)
	gradeRows.Each(func(i int, row *goquery.Selection) {
		description, exists := row.Attr("title")
		if exists {
			descParts := strings.Split(description, " <= ")
			newCnt, _ := strconv.Atoi(descParts[0])
			g, _ := strconv.ParseFloat(descParts[1], 32)
			if g == 5 {
				return
			}
			gradesSum += float32(g) * float32(newCnt-gradesCnt)
			gradesCnt = newCnt
		}
	})
	return gradesSum / float32(gradesCnt)
}

func exitOnError(err error) {
	if err != nil {
		fmt.Print("Fatal error", err)
		os.Exit(1)
	}
}
