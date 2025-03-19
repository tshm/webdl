package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type FormData struct {
	YouTubeURL string
	Email      string
}

type TemplateData struct {
}

func main() {
	http.HandleFunc("/", handleForm)
	http.HandleFunc("/download/", handleDownload)
	fs := http.FileServer(http.Dir("public"))
	http.Handle("/public/", http.StripPrefix("/public/", fs))
	log.Println("Server started on http://localhost:8888")
	log.Fatal(http.ListenAndServe(":8888", nil))
}

func handleForm(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl, err := template.ParseFiles(filepath.Join(".", "main.html"))
		if err != nil {
			log.Println("Error loading template:", err)
			http.Error(w, "Error loading template", http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, nil)
		return
	}

	if r.Method == http.MethodPost {
		log.Println("Download request received")
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Error parsing form", http.StatusInternalServerError)
			return
		}

		formData := FormData{
			YouTubeURL: r.FormValue("youtube_url"),
			Email:      r.FormValue("email"),
		}

		go processDownload(formData, r)
		fmt.Fprintf(w, "Download request received. You will receive an email shortly.")
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func processDownload(formData FormData, r *http.Request) {
	x := getBaseURL(r)
	log.Println("baseurl", x)

	log.Println("Processing download request for:", formData.YouTubeURL)
	uuidstr := uuid.New().String()
	outputDir := "public/" + uuidstr
	err := os.MkdirAll(outputDir, 0755)
	if err != nil {
		log.Println("Error creating directory:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // 10 minutes timeout
	defer cancel()

	cmd := exec.CommandContext(ctx, "yt-dlp", "-x", "--audio-format", "mp3", "-o", filepath.Join(outputDir, "%(title).100B.%(ext)s"), formData.YouTubeURL)

	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	err = cmd.Run()
	if err != nil {
		log.Printf("yt-dlp error: %v, stdout: %s, stderr: %s", err, outb.String(), errb.String())
		sendEmail(formData.Email, "yt-dlp error:\n"+errb.String())
		return
	}

	files, err := filepath.Glob(filepath.Join(outputDir, "*.mp3"))
	if err != nil {
		log.Println("Error finding mp3 files:", err)
		return
	}

	baseURL := getBaseURL(r)

	var downloadLink string = baseURL + "/download/" + uuidstr + "/"
	if len(files) > 1 {
		userName := strings.Split(formData.Email, "@")[0]
		currentTime := time.Now().Format("20060102150405")
		if err != nil {
			log.Println("Error getting current time:", err)
		}
		zipFilename := filepath.Join("public", userName+"_"+currentTime+".zip")
		err = zipFiles(files, zipFilename)
		if err != nil {
			log.Println("Error zipping files:", err)
			return
		}
		downloadLink += filepath.Base(zipFilename)
	} else if len(files) == 1 {
		downloadLink += filepath.Base(files[0])
	} else {
		log.Println("No mp3 files found")
		return
	}
	downloadLink = strings.ReplaceAll(downloadLink, " ", "%20")
	log.Println("Download link:", downloadLink)
	sendEmail(formData.Email, "Download your files here: "+downloadLink)
	log.Println("Download link emailed to:", formData.Email)
	cleanUpOldFiles()
}

func zipFiles(files []string, dest string) error {
	zipFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for _, file := range files {
		fileToZip, err := os.Open(file)
		if err != nil {
			return err
		}
		defer fileToZip.Close()

		info, err := fileToZip.Stat()
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		header.Name = filepath.Base(file)
		header.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		_, err = io.Copy(writer, fileToZip)
		if err != nil {
			return err
		}
	}
	return nil
}

func sendEmail(to, message string) {
	log.Println("Sending email to:", to)
	from := os.Getenv("EMAIL_USER")
	password := os.Getenv("EMAIL_PASSWORD")

	if from == "" || password == "" {
		log.Println("Email credentials not set in environment variables.")
		return
	}

	msg := fmt.Sprintf("Subject: Your download\r\n\r\n%s\r\n", message)

	err := smtp.SendMail("smtp.gmail.com:587",
		smtp.PlainAuth("", from, password, "smtp.gmail.com"),
		from, []string{to}, []byte(msg))

	if err != nil {
		log.Println("Error sending email:", err)
	}
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	log.Println("Download request received")
	filename := strings.TrimPrefix(r.URL.Path, "/download/")
	filePath := filepath.Join("public", filename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filePath)
}

func cleanUpOldFiles() {
	log.Println("Cleaning up old files")
	daysStr := os.Getenv("FILE_RETENTION_DAYS")
	days := 7 // Default retention of 7 days
	if daysStr != "" {
		if parsedDays, err := time.ParseDuration(daysStr + "d"); err == nil {
			days = int(parsedDays.Hours() / 24)
		}
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	filepath.Walk("public", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.ModTime().Before(cutoff) {
			log.Println("Removing old file:", path)
			os.Remove(path)
		}
		return nil
	})
}

// get base url, if server url is http://xxx:888/path, then it should
// return http://xxx:888/.
func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	host := r.Host
	if host == "" {
		host = "localhost:8888" // Fallback to default if Host header is empty
	}

	return scheme + "://" + host
}
