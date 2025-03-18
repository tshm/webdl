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

func main() {
	http.HandleFunc("/", handleForm)
	http.HandleFunc("/download/", handleDownload)
	fs := http.FileServer(http.Dir("public"))
	http.Handle("/public/", http.StripPrefix("/public/", fs))
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleForm(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl := template.Must(template.New("form").Parse(`
<!DOCTYPE html>
<html>
  <head>
    <title>YouTube to MP3</title>
		<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/picnic" />
  </head>
  <body>
    <form method="POST">
      <label for="youtube_url">YouTube URL:</label><br>
      <input type="text" id="youtube_url" name="youtube_url"><br><br>
      <label for="email">Email:</label><br>
      <input type="email" id="email" name="email"><br><br>
      <input type="submit" value="Submit">
    </form>
    <script>
      window.onload = () => {
        const email = document.getElementById("email");
        email.value = localStorage.getItem("email");
        const form = document.querySelector("form");
        form.addEventListener("submit", () => {
            localStorage.setItem("email", email.value); // Save to local storage on submit
        });
      };
    </script>
  </body>
</html>`))
		tmpl.Execute(w, nil)
		return
	}

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Error parsing form", http.StatusInternalServerError)
			return
		}

		formData := FormData{
			YouTubeURL: r.FormValue("youtube_url"),
			Email:      r.FormValue("email"),
		}

		go processDownload(formData)
		fmt.Fprintf(w, "Download request received. You will receive an email shortly.")
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func processDownload(formData FormData) {
	outputDir := "public/" + uuid.New().String()
	err := os.MkdirAll(outputDir, 0755)
	if err != nil {
		log.Println("Error creating directory:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // 10 minutes timeout
	defer cancel()

	cmd := exec.CommandContext(ctx, "yt-dlp", "-x", "--audio-format", "mp3", "-o", filepath.Join(outputDir, "%(title)s.%(ext)s"), formData.YouTubeURL)

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

	var downloadLink string
	if len(files) > 1 {
		userName := strings.Split(formData.Email, "@")[0]
		currentTime := time.Now().Format("20060102150405")
		zipFilename := filepath.Join("public", userName+"_"+currentTime+".zip")
		err = zipFiles(files, zipFilename)
		if err != nil {
			log.Println("Error zipping files:", err)
			return
		}
		downloadLink = "http://localhost:8080/download/" + filepath.Base(zipFilename)
	} else if len(files) == 1 {
		downloadLink = "http://localhost:8080/download/" + filepath.Base(files[0])
	} else {
		log.Println("No mp3 files found")
		return
	}

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
	filename := strings.TrimPrefix(r.URL.Path, "/download/")
	filePath := filepath.Join("public", filename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filePath)
}

func cleanUpOldFiles() {
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
			os.Remove(path)
		}
		return nil
	})
}
