package main

import (
	"fmt"
	"strings"
)

// AudioPlan is the single source of truth for both the compatibility UI and
// the FFmpeg command line. A destination either copies the selected audio
// track or gets its own lightweight audio transcode. No rendition is shared
// between destinations in v0.4.x: isolation and predictable recovery remain
// more important than saving a few AAC encoder cycles.
type AudioPlan struct {
	Mode        string   `json:"mode"` // copy, transcode, unsupported, unknown
	Automatic   bool     `json:"automatic"`
	Codec       string   `json:"codec,omitempty"`
	BitrateKbps int      `json:"bitrate_kbps,omitempty"`
	SampleRate  int      `json:"sample_rate,omitempty"`
	Channels    int      `json:"channels,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
	Label       string   `json:"label"`
}

func audioPlanFor(p Preset, audio *Track) AudioPlan {
	return audioPlanForSetting(p, audio, true)
}

func audioPlanForSetting(p Preset, audio *Track, autoAdapt bool) AudioPlan {
	if audio == nil {
		return AudioPlan{Mode: "unknown", Automatic: false, Label: "Piste audio non détectée"}
	}

	// A custom destination deliberately has no guessed platform constraints.
	if p.ID == "custom" {
		return directAudioPlan(audio)
	}

	c := p.Constraints
	reasons := make([]string, 0, 4)
	codecNeedsChange := len(c.AudioCodecs) > 0 && !containsFold(c.AudioCodecs, audio.CodecName)
	bitrateNeedsChange := audioBitrateExceedsLimit(audio.BitRate, c.MaxAudioBitrateKbps)
	sampleRateNeedsChange := len(c.SampleRates) > 0 && audio.SampleRate > 0 && !containsInt(c.SampleRates, audio.SampleRate)
	channelsNeedChange := c.MaxChannels > 0 && audio.Channels > c.MaxChannels

	if codecNeedsChange {
		reasons = append(reasons, "codec")
	}
	if bitrateNeedsChange {
		reasons = append(reasons, "bitrate")
	}
	if sampleRateNeedsChange {
		reasons = append(reasons, "sample_rate")
	}
	if channelsNeedChange {
		reasons = append(reasons, "channels")
	}

	if len(reasons) == 0 {
		return directAudioPlan(audio)
	}

	if !autoAdapt {
		plan := directAudioPlan(audio)
		plan.Automatic = false
		plan.Reasons = append([]string(nil), reasons...)
		plan.Label = audioPlanLabel(strings.ToLower(strings.TrimSpace(audio.CodecName)), sourceAudioBitrateKbps(audio), audio.SampleRate, audio.Channels)
		return plan
	}

	targetCodec := strings.ToLower(strings.TrimSpace(audio.CodecName))
	if codecNeedsChange {
		targetCodec = pickAutomaticAudioCodec(c.AudioCodecs)
	} else if _, ok := ffmpegAudioEncoder(targetCodec); !ok {
		// We need to re-encode for another reason. Prefer AAC when the preset
		// accepts it, because the bundled FFmpeg always has its native AAC
		// encoder and all currently constrained RTMP presets use AAC.
		if containsFold(c.AudioCodecs, "aac") {
			targetCodec = "aac"
		}
	}

	encoder, automatic := ffmpegAudioEncoder(targetCodec)
	_ = encoder // capability check here; actual encoder name is used below.
	if targetCodec == "" || !automatic {
		return AudioPlan{
			Mode:      "unsupported",
			Automatic: false,
			Codec:     targetCodec,
			Reasons:   reasons,
			Label:     "Adaptation audio non automatisable",
		}
	}

	targetBitrate := sourceAudioBitrateKbps(audio)
	if targetBitrate <= 0 {
		if c.MaxAudioBitrateKbps > 0 {
			targetBitrate = c.MaxAudioBitrateKbps
		} else {
			targetBitrate = 160
		}
	}
	if c.MaxAudioBitrateKbps > 0 && targetBitrate > c.MaxAudioBitrateKbps {
		targetBitrate = c.MaxAudioBitrateKbps
	}

	targetSampleRate := audio.SampleRate
	if sampleRateNeedsChange {
		targetSampleRate = nearestSampleRate(audio.SampleRate, c.SampleRates)
	}

	targetChannels := audio.Channels
	if channelsNeedChange {
		targetChannels = c.MaxChannels
	}

	return AudioPlan{
		Mode:        "transcode",
		Automatic:   true,
		Codec:       targetCodec,
		BitrateKbps: targetBitrate,
		SampleRate:  targetSampleRate,
		Channels:    targetChannels,
		Reasons:     reasons,
		Label:       audioPlanLabel(targetCodec, targetBitrate, targetSampleRate, targetChannels),
	}
}

func directAudioPlan(audio *Track) AudioPlan {
	return AudioPlan{
		Mode:        "copy",
		Automatic:   true,
		Codec:       strings.ToLower(strings.TrimSpace(audio.CodecName)),
		BitrateKbps: sourceAudioBitrateKbps(audio),
		SampleRate:  audio.SampleRate,
		Channels:    audio.Channels,
		Label:       "Copie directe",
	}
}

func sourceAudioBitrateKbps(audio *Track) int {
	if audio == nil || audio.BitRate <= 0 {
		return 0
	}
	return int((audio.BitRate + 500) / 1000)
}

// FFprobe can report a few kb/s above the configured AAC bitrate because of
// estimation and container overhead. Treat up to 4 kb/s (or 2.5%, whichever
// is larger) as measurement tolerance so a nominal 160 kb/s stream observed
// around 164 kb/s is not needlessly re-encoded.
func audioBitrateExceedsLimit(bitRate int64, maxKbps int) bool {
	if bitRate <= 0 || maxKbps <= 0 {
		return false
	}
	observed := float64(bitRate) / 1000.0
	tolerance := float64(maxKbps) * 0.025
	if tolerance < 4 {
		tolerance = 4
	}
	return observed > float64(maxKbps)+tolerance
}

func pickAutomaticAudioCodec(allowed []string) string {
	// Prefer AAC for RTMP/FLV compatibility. More encoders can be added later
	// without changing the planning API.
	for _, preferred := range []string{"aac"} {
		if containsFold(allowed, preferred) {
			return preferred
		}
	}
	return ""
}

func ffmpegAudioEncoder(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "aac":
		return "aac", true
	default:
		return "", false
	}
}

func nearestSampleRate(source int, allowed []int) int {
	if len(allowed) == 0 {
		return source
	}
	if source <= 0 {
		return allowed[0]
	}
	best := allowed[0]
	bestDistance := absInt(source - best)
	for _, candidate := range allowed[1:] {
		d := absInt(source - candidate)
		if d < bestDistance {
			best = candidate
			bestDistance = d
		}
	}
	return best
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func audioPlanLabel(codec string, bitrateKbps, sampleRate, channels int) string {
	parts := []string{prettyCodec(codec)}
	if bitrateKbps > 0 {
		parts = append(parts, fmt.Sprintf("%d kb/s", bitrateKbps))
	}
	if sampleRate > 0 {
		parts = append(parts, prettySampleRate(sampleRate))
	}
	if channels > 0 {
		parts = append(parts, channelLabel(channels))
	}
	return strings.Join(parts, " · ")
}

func channelLabel(channels int) string {
	switch channels {
	case 1:
		return "mono"
	case 2:
		return "stéréo"
	default:
		return fmt.Sprintf("%d canaux", channels)
	}
}

func audioFFmpegArgs(plan AudioPlan, source *Track) ([]string, error) {
	switch plan.Mode {
	case "copy":
		return []string{"-c:a", "copy"}, nil
	case "transcode":
		encoder, ok := ffmpegAudioEncoder(plan.Codec)
		if !ok {
			return nil, fmt.Errorf("no automatic FFmpeg encoder configured for audio codec %q", plan.Codec)
		}
		args := []string{"-c:a", encoder}
		if plan.BitrateKbps > 0 {
			args = append(args, "-b:a", fmt.Sprintf("%dk", plan.BitrateKbps))
		}
		if plan.SampleRate > 0 && (source == nil || plan.SampleRate != source.SampleRate) {
			args = append(args, "-ar", fmt.Sprintf("%d", plan.SampleRate))
		}
		if plan.Channels > 0 && (source == nil || plan.Channels != source.Channels) {
			args = append(args, "-ac", fmt.Sprintf("%d", plan.Channels))
		}
		return args, nil
	case "unsupported":
		return nil, fmt.Errorf("audio adaptation required but not automatically supported")
	default:
		return nil, fmt.Errorf("audio processing plan is not available")
	}
}

func buildDestinationFFmpegArgs(src, out string, d Destination, p Preset, video, audio *Track) ([]string, AudioPlan, error) {
	if video == nil {
		return nil, AudioPlan{}, fmt.Errorf("selected video track 0:v:%d is not available", d.VideoTrack)
	}
	if audio == nil {
		return nil, AudioPlan{}, fmt.Errorf("selected audio track 0:a:%d is not available", d.AudioTrack)
	}
	plan := audioPlanForSetting(p, audio, d.AutoAdaptAudio)
	audioArgs, err := audioFFmpegArgs(plan, audio)
	if err != nil {
		return nil, plan, err
	}

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-rtmp_enhanced_codecs", enhancedCodecs,
		"-i", src,
		"-map", fmt.Sprintf("0:v:%d", d.VideoTrack),
		"-map", fmt.Sprintf("0:a:%d", d.AudioTrack),
		"-c:v", "copy",
	}
	args = append(args, audioArgs...)
	args = append(args,
		"-flvflags", "no_duration_filesize",
		"-f", "flv", out,
	)
	return args, plan, nil
}
