#!/bin/sh
set -eu
dir="${1:-testdata/audio}"
duration="${GODROME_SAMPLE_DURATION:-4}"
mkdir -p "$dir"
for format in mp3 flac m4a opus; do
  case "$format" in
    mp3) codec=libmp3lame ;;
    flac) codec=flac ;;
    m4a) codec=aac ;;
    opus) codec=libopus ;;
  esac
  ffmpeg -hide_banner -loglevel error -y -f lavfi -i "sine=frequency=440:duration=$duration:sample_rate=48000" -c:a "$codec" "$dir/sample.$format"
done
