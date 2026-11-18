package main

import "github.com/mediactl/clustarr/pkg/torznab"

func leechers(r torznab.Release) *int32 {
	if r.Leechers != nil && *r.Leechers >= 0 {
		return r.Leechers
	}
	if r.Peers != nil && r.Seeders != nil && *r.Seeders >= 0 && *r.Peers >= *r.Seeders {
		n := *r.Peers - *r.Seeders
		return &n
	}
	return nil
}
func category(r torznab.Release) string {
	for _, c := range r.Categories {
		if c == 5070 {
			return "anime"
		}
	}
	for _, c := range r.Categories {
		switch c / 1000 {
		case 2:
			return "movie"
		case 3:
			return "music"
		case 4:
			return "software"
		case 5:
			return "series"
		case 7:
			return "book"
		}
	}
	if len(r.Categories) > 0 {
		return "other"
	}
	return ""
}
