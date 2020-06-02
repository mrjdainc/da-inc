package providers

import (
	"github.com/mrjdainc/da-inc/bittorrent"
	"github.com/mrjdainc/da-inc/tmdb"
)

// Searcher ...
type Searcher interface {
	SearchLinks(query string) []*bittorrent.TorrentFile
}

// MovieSearcher ...
type MovieSearcher interface {
	SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile
	SearchMovieLinksSilent(movie *tmdb.Movie, withAuth bool) []*bittorrent.TorrentFile
}

// SeasonSearcher ...
type SeasonSearcher interface {
	SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile
}

// EpisodeSearcher ...
type EpisodeSearcher interface {
	SearchEpisodeLinks(show *tmdb.Show, episode *tmdb.Episode) []*bittorrent.TorrentFile
}
