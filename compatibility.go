package main

import (
	"fmt"
	"math"
	"strings"
)

type CompatibilityIssue struct {
	Kind     string `json:"kind"`     // video, audio, recommendation, unknown
	Severity string `json:"severity"` // info, warning, adapt_audio, transcode_video
	Message  string `json:"message"`
}

type CompatibilityResult struct {
	Status    string               `json:"status"` // direct, warning, adapt_audio, transcode_video, unknown
	Label     string               `json:"label"`
	Issues    []CompatibilityIssue `json:"issues,omitempty"`
	AudioPlan AudioPlan            `json:"audio_plan"`
}

func compatibilityFor(p Preset, video, audio *Track) CompatibilityResult {
	return compatibilityForSetting(p, video, audio, true)
}

func compatibilityForSetting(p Preset, video, audio *Track, autoAdaptAudio bool) CompatibilityResult {
	audioPlan := audioPlanForSetting(p, audio, autoAdaptAudio)
	if p.ID == "custom" {
		return CompatibilityResult{Status: "unknown", Label: "Compatibilité non contrainte", AudioPlan: audioPlan}
	}
	issues := []CompatibilityIssue{}
	if video == nil {
		issues = append(issues, CompatibilityIssue{Kind: "video", Severity: "unknown", Message: "Piste vidéo non détectée actuellement"})
	} else {
		c := p.Constraints
		if len(c.VideoCodecs) > 0 && !containsFold(c.VideoCodecs, video.CodecName) {
			issues = append(issues, CompatibilityIssue{Kind: "video", Severity: "transcode_video", Message: fmt.Sprintf("Codec vidéo %s non prévu par %s", prettyCodec(video.CodecName), p.Name)})
		}
		if c.MaxVideoBitrateKbps > 0 {
			if video.BitRate <= 0 {
				issues = append(issues, CompatibilityIssue{Kind: "unknown", Severity: "info", Message: "Débit vidéo non exposé par FFprobe : limite impossible à vérifier"})
			} else if int((video.BitRate+999)/1000) > c.MaxVideoBitrateKbps {
				sev := "warning"
				if c.VideoBitrateIsHardLimit {
					sev = "transcode_video"
				}
				issues = append(issues, CompatibilityIssue{Kind: "video", Severity: sev, Message: fmt.Sprintf("Débit vidéo ~%d kb/s supérieur à la limite %d kb/s", (video.BitRate+500)/1000, c.MaxVideoBitrateKbps)})
			}
		}
		fps := trackFPS(*video)
		if c.MaxFPS > 0 && fps > c.MaxFPS+0.01 {
			issues = append(issues, CompatibilityIssue{Kind: "video", Severity: "transcode_video", Message: fmt.Sprintf("%.2g fps détectés ; %s demande au plus %.2g fps", fps, p.Name, c.MaxFPS)})
		}
		if c.MaxWidth > 0 && video.Width > c.MaxWidth || c.MaxHeight > 0 && video.Height > c.MaxHeight {
			issues = append(issues, CompatibilityIssue{Kind: "video", Severity: "transcode_video", Message: fmt.Sprintf("Résolution %d×%d supérieure aux limites du preset", video.Width, video.Height)})
		}
		if c.RecommendedProfile != "" && video.Profile != "" && !strings.EqualFold(video.Profile, c.RecommendedProfile) {
			issues = append(issues, CompatibilityIssue{Kind: "recommendation", Severity: "warning", Message: fmt.Sprintf("Profil H.264 %s détecté ; %s recommande %s", video.Profile, p.Name, c.RecommendedProfile)})
		}
		if c.MaxBFrames != nil && video.HasBFrames > *c.MaxBFrames {
			issues = append(issues, CompatibilityIssue{Kind: "video", Severity: "transcode_video", Message: fmt.Sprintf("%d B-frame(s) détectée(s) ; le preset en autorise %d", video.HasBFrames, *c.MaxBFrames)})
		}
		if c.Orientation != "" && c.Orientation != "any" && video.Width > 0 && video.Height > 0 {
			orientation := "portrait"
			if video.Width >= video.Height {
				orientation = "landscape"
			}
			if orientation != c.Orientation {
				want := "portrait"
				if c.Orientation == "landscape" {
					want = "paysage"
				}
				issues = append(issues, CompatibilityIssue{Kind: "recommendation", Severity: "warning", Message: fmt.Sprintf("Rendition %s ; ce preset est prévu en %s", map[string]string{"portrait": "portrait", "landscape": "paysage"}[orientation], want)})
			}
		}
	}

	if audio == nil {
		issues = append(issues, CompatibilityIssue{Kind: "audio", Severity: "unknown", Message: "Piste audio non détectée actuellement"})
	} else {
		c := p.Constraints
		if len(c.AudioCodecs) > 0 && !containsFold(c.AudioCodecs, audio.CodecName) {
			issues = append(issues, CompatibilityIssue{Kind: "audio", Severity: "adapt_audio", Message: fmt.Sprintf("Codec audio %s à adapter pour %s", prettyCodec(audio.CodecName), p.Name)})
		}
		if c.MaxAudioBitrateKbps > 0 {
			if audio.BitRate <= 0 {
				issues = append(issues, CompatibilityIssue{Kind: "unknown", Severity: "info", Message: "Débit audio non exposé par FFprobe : limite impossible à vérifier"})
			} else if audioBitrateExceedsLimit(audio.BitRate, c.MaxAudioBitrateKbps) {
				issues = append(issues, CompatibilityIssue{Kind: "audio", Severity: "adapt_audio", Message: fmt.Sprintf("Audio ~%d kb/s ; limite %d kb/s", (audio.BitRate+500)/1000, c.MaxAudioBitrateKbps)})
			}
		}
		if len(c.SampleRates) > 0 && audio.SampleRate > 0 && !containsInt(c.SampleRates, audio.SampleRate) {
			issues = append(issues, CompatibilityIssue{Kind: "audio", Severity: "adapt_audio", Message: fmt.Sprintf("Fréquence audio %s à adapter", prettySampleRate(audio.SampleRate))})
		}
		if c.MaxChannels > 0 && audio.Channels > c.MaxChannels {
			issues = append(issues, CompatibilityIssue{Kind: "audio", Severity: "adapt_audio", Message: fmt.Sprintf("%d canaux détectés ; maximum %d", audio.Channels, c.MaxChannels)})
		}
	}

	status := "direct"
	label := "Copie directe"
	for _, issue := range issues {
		switch issue.Severity {
		case "transcode_video":
			status, label = "transcode_video", "Transcodage vidéo requis"
			return CompatibilityResult{Status: status, Label: label, Issues: issues, AudioPlan: audioPlan}
		case "adapt_audio":
			if status != "transcode_video" {
				switch {
				case audioPlan.Mode == "transcode" && audioPlan.Automatic:
					status, label = "adapt_audio", "Compatible — adaptation audio automatique"
				case audioPlan.Mode == "copy" && len(audioPlan.Reasons) > 0 && !autoAdaptAudio:
					status, label = "warning", "Audio hors contraintes — adaptation désactivée"
				default:
					status, label = "adapt_audio", "Adaptation audio requise"
				}
			}
		case "warning":
			if status == "direct" {
				status, label = "warning", "Compatible, hors recommandation"
			}
		case "unknown":
			if status == "direct" {
				status, label = "unknown", "Compatibilité incomplète"
			}
		}
	}
	return CompatibilityResult{Status: status, Label: label, Issues: issues, AudioPlan: audioPlan}
}

func containsFold(values []string, needle string) bool {
	for _, v := range values {
		if strings.EqualFold(v, needle) {
			return true
		}
	}
	return false
}

func containsInt(values []int, needle int) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func trackFPS(t Track) float64 {
	parts := strings.Split(t.FrameRate, "/")
	if len(parts) != 2 {
		return 0
	}
	n := parseFloat(parts[0])
	d := parseFloat(parts[1])
	if d == 0 {
		return 0
	}
	return n / d
}

func parseFloat(v string) float64 {
	var f float64
	_, _ = fmt.Sscanf(v, "%f", &f)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}
