package api

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	lt "github.com/da-inc/libtorrent-go"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/mrjdainc/da-inc/bittorrent"
	"github.com/mrjdainc/da-inc/config"
	"github.com/mrjdainc/da-inc/database"
	"github.com/mrjdainc/da-inc/util"
	"github.com/mrjdainc/da-inc/xbmc"
)

var (
	torrentsLog    = logging.MustGetLogger("torrents")
	cachedTorrents = map[int]string{}
)

// TorrentsWeb ...
type TorrentsWeb struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Size          string  `json:"size"`
	Status        string  `json:"status"`
	Progress      float64 `json:"progress"`
	Ratio         float64 `json:"ratio"`
	TimeRatio     float64 `json:"time_ratio"`
	SeedingTime   string  `json:"seeding_time"`
	SeedTime      float64 `json:"seed_time"`
	SeedTimeLimit int     `json:"seed_time_limit"`
	DownloadRate  float64 `json:"download_rate"`
	UploadRate    float64 `json:"upload_rate"`
	Seeders       int     `json:"seeders"`
	SeedersTotal  int     `json:"seeders_total"`
	Peers         int     `json:"peers"`
	PeersTotal    int     `json:"peers_total"`
}

// AddToTorrentsMap ...
func AddToTorrentsMap(tmdbID string, torrent *bittorrent.TorrentFile) {
	if strings.HasPrefix(torrent.URI, "magnet") {
		torrentsLog.Debugf("Saving torrent entry for TMDB: %#v", tmdbID)
		if b, err := torrent.MarshalJSON(); err == nil {
			database.GetStorm().AddTorrentLink(tmdbID, torrent.InfoHash, b)
		}

		return
	}

	b, err := ioutil.ReadFile(torrent.URI)
	if err != nil {
		return
	}

	torrentsLog.Debugf("Saving torrent entry for TMDB: %#v", tmdbID)
	database.GetStorm().AddTorrentLink(tmdbID, torrent.InfoHash, b)
}

// InTorrentsMap ...
func InTorrentsMap(tmdbID string) *bittorrent.TorrentFile {
	if !config.Get().UseCacheSelection || tmdbID == "" {
		return nil
	}

	tmdbInt, _ := strconv.Atoi(tmdbID)
	var ti database.TorrentAssignItem
	var tm database.TorrentAssignMetadata
	if err := database.GetStormDB().One("TmdbID", tmdbInt, &ti); err != nil {
		return nil
	}
	if err := database.GetStormDB().One("InfoHash", ti.InfoHash, &tm); err != nil {
		return nil
	}

	torrent := &bittorrent.TorrentFile{}
	if tm.Metadata[0] == '{' {
		torrent.UnmarshalJSON(tm.Metadata)
	} else {
		torrent.LoadFromBytes(tm.Metadata)
	}

	if len(torrent.URI) > 0 && (config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("dainc", fmt.Sprintf("LOCALIZE[30260];;[COLOR gold]%s[/COLOR]", torrent.Title))) {
		return torrent
	}

	database.GetStormDB().DeleteStruct(&ti)
	database.GetStorm().CleanupTorrentLink(ti.InfoHash)

	return nil
}

// InTorrentsHistory ...
func InTorrentsHistory(infohash string) *bittorrent.TorrentFile {
	if !config.Get().UseTorrentHistory || infohash == "" {
		return nil
	}

	var th database.TorrentHistory
	if err := database.GetStormDB().One("InfoHash", infohash, &th); err != nil {
		return nil
	}

	if len(infohash) > 0 && len(th.Metadata) > 0 {
		torrent := &bittorrent.TorrentFile{}
		if th.Metadata[0] == '{' {
			torrent.UnmarshalJSON(th.Metadata)
		} else {
			torrent.LoadFromBytes(th.Metadata)
		}

		if len(torrent.URI) > 0 {
			return torrent
		}
	}

	return nil
}

// GetCachedTorrents searches for torrent entries in the cache
func GetCachedTorrents(tmdbID string) ([]*bittorrent.TorrentFile, error) {
	if !config.Get().UseCacheSearch {
		return nil, fmt.Errorf("Caching is disabled")
	}

	cacheDB := database.GetCache()

	var ret []*bittorrent.TorrentFile
	err := cacheDB.GetCachedObject(database.CommonBucket, tmdbID, &ret)
	if len(ret) > 0 {
		for _, t := range ret {
			if !strings.HasPrefix(t.URI, "magnet:") {
				if _, err = os.Open(t.URI); err != nil {
					return nil, fmt.Errorf("Cache is not up to date")
				}
			}
		}
	}

	return ret, err
}

// SetCachedTorrents caches torrent search results in cache
func SetCachedTorrents(tmdbID string, torrents []*bittorrent.TorrentFile) error {
	cacheDB := database.GetCache()

	return cacheDB.SetCachedObject(database.CommonBucket, config.Get().CacheSearchDuration, tmdbID, torrents)
}

// ListTorrents ...
func ListTorrents(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items := make(xbmc.ListItems, 0, len(s.GetTorrents()))
		if len(s.GetTorrents()) == 0 {
			ctx.JSON(200, xbmc.NewView("", items))
			return
		}

		// torrentsLog.Debug("Currently downloading:")
		for _, t := range s.GetTorrents() {
			if t == nil {
				continue
			}

			torrentName := t.Name()
			progress := t.GetProgress()
			status := t.GetStateString()
			// dt := t.GetAddedTime()

			torrentAction := []string{"LOCALIZE[30231]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/pause/%s", t.InfoHash()))}
			sessionAction := []string{"LOCALIZE[30233]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/pause"))}

			if s.Session.IsPaused() {
				sessionAction = []string{"LOCALIZE[30234]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/resume"))}
			} else if t.GetPaused() {
				torrentAction = []string{"LOCALIZE[30235]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/resume/%s", t.InfoHash()))}
			}

			color := "white"
			switch status {
			case bittorrent.StatusStrings[bittorrent.StatusPaused]:
				fallthrough
			case bittorrent.StatusStrings[bittorrent.StatusFinished]:
				color = "grey"
			case bittorrent.StatusStrings[bittorrent.StatusSeeding]:
				color = "green"
			case bittorrent.StatusStrings[bittorrent.StatusBuffering]:
				color = "blue"
			case bittorrent.StatusStrings[bittorrent.StatusFinding]:
				color = "orange"
			case bittorrent.StatusStrings[bittorrent.StatusChecking]:
				color = "teal"
			case bittorrent.StatusStrings[bittorrent.StatusFinding]:
				color = "orange"
			case bittorrent.StatusStrings[bittorrent.StatusAllocating]:
				color = "black"
			case bittorrent.StatusStrings[bittorrent.StatusStalled]:
				color = "red"
			}

			playURL := t.GetPlayURL("")

			item := xbmc.ListItem{
				Label: fmt.Sprintf("%.2f%% - [COLOR %s]%s[/COLOR] - %s", progress, color, status, torrentName),
				Path:  playURL,
				Info: &xbmc.ListItemInfo{
					Title: torrentName,
				},
			}

			item.ContextMenu = [][]string{
				[]string{"LOCALIZE[30230]", fmt.Sprintf("XBMC.PlayMedia(%s)", playURL)},
				torrentAction,
				[]string{"LOCALIZE[30232]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/delete/%s", t.InfoHash()))},
				[]string{"LOCALIZE[30276]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/delete/%s?files=true", t.InfoHash()))},
				[]string{"LOCALIZE[30308]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/move/%s", t.InfoHash()))},
				sessionAction,
			}

			if !s.IsMemoryStorage() {
				item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30573]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/selectfile/%s", t.InfoHash()))})

				if t.HasAvailableFiles() {
					item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30531]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/downloadall/%s", t.InfoHash()))})
				} else {
					item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30532]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/torrents/undownloadall/%s", t.InfoHash()))})
				}
			}

			item.IsPlayable = true
			items = append(items, &item)
		}

		ctx.JSON(200, xbmc.NewView("", items))
	}
}

// ListTorrentsWeb ...
func ListTorrentsWeb(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if s.Closer.IsSet() {
			return
		}

		// TODO: Need to rewrite all this lists to use Service.[]Torrent
		torrentsVector := s.Session.GetTorrents()
		torrentsVectorSize := int(torrentsVector.Size())
		torrents := make([]*TorrentsWeb, 0, torrentsVectorSize)
		seedTimeLimit := config.Get().SeedTimeLimit

		if torrentsVectorSize == 0 {
			ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			ctx.JSON(200, torrents)
			return
		}

		for _, t := range s.GetTorrents() {
			th := t.GetHandle()
			if th == nil || !th.IsValid() {
				continue
			}

			torrentStatus := th.Status()
			defer lt.DeleteTorrentStatus(torrentStatus)

			torrentName := torrentStatus.GetName()
			progress := float64(torrentStatus.GetProgress()) * 100

			infoHash := t.InfoHash()
			status := t.GetStateString()

			ratio := float64(0)
			allTimeDownload := float64(torrentStatus.GetAllTimeDownload())
			if allTimeDownload > 0 {
				ratio = float64(torrentStatus.GetAllTimeUpload()) / allTimeDownload
			}

			timeRatio := float64(0)
			finishedTime := float64(torrentStatus.GetFinishedTime())
			downloadTime := float64(torrentStatus.GetActiveTime()) - finishedTime
			if downloadTime > 1 {
				timeRatio = finishedTime / downloadTime
			}
			seedingTime := time.Duration(torrentStatus.GetSeedingTime()) * time.Second
			if progress == 100 && seedingTime == 0 {
				seedingTime = time.Duration(finishedTime) * time.Second
			}

			size := humanize.Bytes(uint64(t.Length()))

			downloadRate := float64(torrentStatus.GetDownloadPayloadRate()) / 1024
			uploadRate := float64(torrentStatus.GetUploadPayloadRate()) / 1024

			seeders, seedersTotal, peers, peersTotal := t.GetConnections()

			ti := &TorrentsWeb{
				ID:            infoHash,
				Name:          torrentName,
				Size:          size,
				Status:        status,
				Progress:      progress,
				Ratio:         ratio,
				TimeRatio:     timeRatio,
				SeedingTime:   seedingTime.String(),
				SeedTime:      seedingTime.Seconds(),
				SeedTimeLimit: seedTimeLimit,
				DownloadRate:  downloadRate,
				UploadRate:    uploadRate,
				Seeders:       seeders,
				SeedersTotal:  seedersTotal,
				Peers:         peers,
				PeersTotal:    peersTotal,
			}
			torrents = append(torrents, ti)
		}

		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.JSON(200, torrents)
	}
}

// PauseSession ...
func PauseSession(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO: Add Global Pause
		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// ResumeSession ...
func ResumeSession(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO: Add Global Resume
		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// AddTorrent ...
func AddTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Request.FormValue("uri")
		file, header, fileError := ctx.Request.FormFile("file")
		allFiles := ctx.Request.FormValue("all")

		if file != nil && header != nil && fileError == nil {
			t, err := saveTorrentFile(file, header)
			if err == nil && t != "" {
				uri = t
			}
		}

		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		if uri == "" {
			torrentsLog.Errorf("Torrent file/magnet url is empty")
			ctx.String(404, "Missing torrent URI")
			return
		}
		torrentsLog.Infof("Adding torrent from %s", uri)

		t, err := s.AddTorrent(uri, false)
		if err != nil {
			ctx.String(404, err.Error())
			return
		}

		torrentsLog.Infof("Downloading %s", uri)
		if allFiles == "1" {
			// Selecting all files
			torrentsLog.Infof("Selecting all files for download")
			t.DownloadAllFiles()
		} else {
			file, _, err := t.ChooseFile(nil)
			if err == nil && file != nil {
				t.DownloadFile(file)
			} else {
				torrentsLog.Errorf("File was not selected")
			}
		}

		xbmc.Refresh()
		ctx.String(200, "")
	}
}

// ResumeTorrent ...
func ResumeTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to resume torrent with index %s", torrentID))
			return
		}

		torrent.Resume()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// MoveTorrent ...
func MoveTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to move torrent with index %s", torrentID))
			return
		}

		torrentsLog.Infof("Marking %s to be moved...", torrent.Name())
		s.MarkedToMove = torrent.InfoHash()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// PauseTorrent ...
func PauseTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to pause torrent with index %s", torrentID))
			return
		}

		torrent.Pause()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// RemoveTorrent ...
func RemoveTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		deleteFiles := ctx.Query("files")

		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to remove torrent with index %s", torrentID))
			return
		}

		s.RemoveTorrent(torrent, true, deleteFiles != "", false)

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// DownloadAllTorrent ...
func DownloadAllTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to download all files for torrent with index %s", torrentID))
			return
		}

		torrent.DownloadAllFiles()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// UnDownloadAllTorrent ...
func UnDownloadAllTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to undownload all files for torrent with index %s", torrentID))
			return
		}

		torrent.UnDownloadAllFiles()

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// SelectFileTorrent ...
func SelectFileTorrent(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		torrent, err := GetTorrentFromParam(s, torrentID)
		if err != nil {
			ctx.Error(fmt.Errorf("Unable to select files for torrent with index %s", torrentID))
			return
		}

		file, choice, err := torrent.ChooseFile(nil)
		if err == nil && file != nil {
			url := torrent.GetPlayURL(strconv.Itoa(choice))
			log.Debugf("Triggering play for: %s", url)
			xbmc.PlayURL(url)
			return
		}

		xbmc.Refresh()
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}

// Versions ...
func Versions(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		type Versions struct {
			Version   string `json:"version"`
			UserAgent string `json:"user-agent"`
		}
		versions := Versions{
			Version:   util.GetVersion(),
			UserAgent: s.UserAgent,
		}
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.JSON(200, versions)
	}
}

// GetTorrentFromParam ...
func GetTorrentFromParam(s *bittorrent.Service, param string) (*bittorrent.Torrent, error) {
	if len(param) == 0 {
		return nil, errors.New("Empty param")
	}

	t := s.GetTorrentByHash(param)
	if t == nil {
		return nil, errors.New("Torrent not found")
	}
	return t, nil
}

func saveTorrentFile(file multipart.File, header *multipart.FileHeader) (string, error) {
	if file == nil || header == nil {
		return "", fmt.Errorf("Not a valid file entry")
	}

	var err error
	path := filepath.Join(config.Get().TorrentsPath, filepath.Base(header.Filename))
	log.Debugf("Saving incoming torrent file to: %s", path)

	if _, err = os.Stat(path); err != nil && !os.IsNotExist(err) {
		err = os.Remove(path)
		if err != nil {
			return "", fmt.Errorf("Could not remove the file: %s", err)
		}
	}

	out, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("Could not create file: %s", err)
	}
	defer out.Close()
	if _, err = io.Copy(out, file); err != nil {
		return "", fmt.Errorf("Could not write file content: %s", err)
	}

	return path, nil
}
