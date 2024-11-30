package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beekeeper1010/lvs2/server"

	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan mp4 files to generate sqlite db file",
	Long:  "Scan mp4 files to generate sqlite db file",
	Run: func(cmd *cobra.Command, args []string) {
		if err := exec.Command("ffmpeg", "-version").Run(); err != nil {
			fmt.Println("ffmpeg not found, please install")
			return
		}
		dirs, _ := cmd.Flags().GetStringArray("dir")
		filter, _ := cmd.Flags().GetInt("filter")
		height, _ := cmd.Flags().GetInt("height")
		dbfile, _ := cmd.Flags().GetString("db")
		if err := scanMp4Files(dirs, filter, max(height, 100), dbfile); err != nil {
			fmt.Println(err)
		}
	},
}

func init() {
	scanCmd.Flags().String("db", "lvs2.db", "sqlite db file for result")
	scanCmd.Flags().StringArrayP("dir", "d", nil, "dir to scan")
	scanCmd.MarkFlagRequired("dir")
	scanCmd.Flags().IntP("filter", "f", 60, "skip mp4 files which duration is less than this value(seconds)")
	scanCmd.Flags().Int("height", 100, "height of thumbnail, min 100")
	rootCmd.AddCommand(scanCmd)
}

func scanMp4Files(dirs []string, filter, height int, dbfile string) error {
	mp4Files := make([]*server.Mp4File, 0, 1000)
	for _, dir := range dirs {
		fmt.Println("scanning", dir)
		filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				fmt.Println(err)
				return filepath.SkipDir
			}
			if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".mp4" {
				return nil
			}
			fileInfo, err := os.Stat(path)
			if err != nil {
				fmt.Println(err)
				return filepath.SkipDir
			}
			duration, err := getDuration(path)
			if err != nil {
				fmt.Println(err)
				return filepath.SkipDir
			}
			if duration < filter {
				return nil
			}
			thumbnail, err := getThumbnailBase64(path, duration>>1, height)
			if err != nil {
				fmt.Println(err)
				return filepath.SkipDir
			}
			fmt.Printf("found %s, duration=%ds\n", path, duration)
			mp4Files = append(mp4Files, &server.Mp4File{
				Name:      entry.Name(),
				Path:      path,
				Size:      fileInfo.Size(),
				Duration:  duration,
				Thumbnail: thumbnail,
			})
			return nil
		})
	}
	if len(mp4Files) == 0 {
		fmt.Println("no mp4 files found")
		return nil
	}
	if err := server.InitializeDb(dbfile); err != nil {
		return err
	}
	server.DB.Migrator().DropTable(&server.Mp4File{})
	if err := server.InitializeTable(); err != nil {
		return err
	}
	result := server.DB.Create(mp4Files)
	if result.Error == nil {
		fmt.Println("inserted", result.RowsAffected, "record(s) to", dbfile)
	}
	return result.Error
}

func getDuration(path string) (int, error) {
	command := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	out, err := command.Output()
	if err != nil {
		return 0.0, err
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return int(duration), err
}

func getThumbnailBase64(path string, offset, height int) (string, error) {
	tmpPng := filepath.Join(os.TempDir(), "tmp.png")
	command := exec.Command("ffmpeg", "-v", "error", "-ss", strconv.Itoa(offset), "-i", path, "-vframes", "1", "-vf", fmt.Sprintf("scale=-1:%d", height), "-y", tmpPng)
	if _, err := command.Output(); err != nil {
		return "", err
	}
	if _, err := os.Stat(tmpPng); err != nil {
		return "", err
	}
	data, err := os.ReadFile(tmpPng)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}
