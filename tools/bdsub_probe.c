#define _FILE_OFFSET_BITS 64
#define _POSIX_C_SOURCE 200809L

#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>

#define STREAM_TYPE_MPEG1_VIDEO 0x01
#define STREAM_TYPE_MPEG2_VIDEO 0x02
#define STREAM_TYPE_AVC_VIDEO 0x1B
#define STREAM_TYPE_MVC_VIDEO 0x20
#define STREAM_TYPE_HEVC_VIDEO 0x24
#define STREAM_TYPE_VC1_VIDEO 0xEA
#define STREAM_TYPE_PRESENTATION_GRAPHICS 0x90
#define STREAM_TYPE_INTERACTIVE_GRAPHICS 0x91
#define M2TS_PACKET_SIZE 192
#define TS_PACKET_SIZE 188
#define M2TS_TS_SYNC_OFFSET 4
#define M2TS_READ_CHUNK (M2TS_PACKET_SIZE * 4096)
#define M2TS_SAMPLE_WINDOW_COUNT 32u
#define M2TS_SAMPLE_WINDOW_BYTES (2u * 1024u * 1024u)

typedef struct pg_stream_info {
    uint16_t pid;
    char lang[4];
    uint8_t coding_type;
    uint8_t char_code;
    uint8_t subpath_id;
    uint64_t payload_bytes;
    uint64_t bitrate;
} PG_STREAM_INFO;

typedef struct probe_result {
    char source[32];
    char bitrate_mode[32];
    uint32_t playlist;
    uint32_t clip_ref;
    uint32_t clip_in_time_ticks;
    uint32_t clip_out_time_ticks;
    uint32_t clip_duration_ticks;
    double clip_packet_seconds;
    int bitrate_scanned;
    char clip_id[6];
    size_t video_stream_count;
    uint16_t *video_streams;
    size_t pg_stream_count;
    PG_STREAM_INFO *pg_streams;
} PROBE_RESULT;

typedef struct scan_pid_state {
    uint16_t pid;
    uint8_t kind;
    uint32_t parse;
    int transfer_state;
    int packet_length;
    int packet_length_variable;
    uint8_t packet_length_parse;
    uint8_t packet_parse;
    uint8_t pes_header_length;
    uint8_t pes_header_flags;
    uint8_t pts_parse;
    uint64_t pts_temp;
    uint64_t pts_last;
    uint64_t dts_prev;
    uint64_t dts_temp;
    uint8_t dts_parse;
    uint64_t pts_count;
    PG_STREAM_INFO *pg_stream;
} SCAN_PID_STATE;

enum {
    SCAN_KIND_VIDEO = 1,
    SCAN_KIND_GRAPHICS = 2
};

static void usage(const char *argv0)
{
    fprintf(stderr,
            "usage: %s <disc-path> --playlist <mpls> [--clip <clip-id>] [--scan-payload] [--scan-bitrate]\n",
            argv0);
}

static int parse_u32(const char *text, uint32_t *out)
{
    char *end = NULL;
    unsigned long value;

    if (!text || !*text || !out) {
        return 0;
    }

    errno = 0;
    value = strtoul(text, &end, 10);
    if (errno != 0 || !end || *end != '\0' || value > UINT32_MAX) {
        return 0;
    }

    *out = (uint32_t)value;
    return 1;
}

static void log_stagef(const char *format, ...)
{
    va_list args;

    va_start(args, format);
    vfprintf(stderr, format, args);
    fputc('\n', stderr);
    fflush(stderr);
    va_end(args);
}

static int stream_type_is_video(uint8_t stream_type)
{
    switch (stream_type) {
    case STREAM_TYPE_MPEG1_VIDEO:
    case STREAM_TYPE_MPEG2_VIDEO:
    case STREAM_TYPE_AVC_VIDEO:
    case STREAM_TYPE_MVC_VIDEO:
    case STREAM_TYPE_HEVC_VIDEO:
    case STREAM_TYPE_VC1_VIDEO:
        return 1;
    default:
        return 0;
    }
}

static uint64_t round_to_u64(double value)
{
    if (!(value > 0.0)) {
        return 0;
    }
    return (uint64_t)(value + 0.5);
}

static uint16_t read_be16(const uint8_t *data, size_t pos)
{
    return (uint16_t)(((uint16_t)data[pos] << 8) | data[pos + 1]);
}

static uint32_t read_be32(const uint8_t *data, size_t pos)
{
    return ((uint32_t)data[pos] << 24) |
           ((uint32_t)data[pos + 1] << 16) |
           ((uint32_t)data[pos + 2] << 8) |
           (uint32_t)data[pos + 3];
}

static void json_print_string(const char *text)
{
    const unsigned char *p = (const unsigned char *)(text ? text : "");

    putchar('"');
    while (*p) {
        switch (*p) {
        case '\\':
        case '"':
            putchar('\\');
            putchar((int)*p);
            break;
        case '\b':
            fputs("\\b", stdout);
            break;
        case '\f':
            fputs("\\f", stdout);
            break;
        case '\n':
            fputs("\\n", stdout);
            break;
        case '\r':
            fputs("\\r", stdout);
            break;
        case '\t':
            fputs("\\t", stdout);
            break;
        default:
            if (*p < 0x20) {
                printf("\\u%04x", *p);
            } else {
                putchar((int)*p);
            }
            break;
        }
        p++;
    }
    putchar('"');
}

static int lang_is_present(const char *lang)
{
    return lang && lang[0] != '\0';
}

static void normalize_lang_code(const uint8_t *data, char out[4])
{
    size_t i;

    out[0] = '\0';
    if (!data) {
        return;
    }

    for (i = 0; i < 3; i++) {
        unsigned char ch = data[i];
        if (ch == '\0' || isspace(ch) || !isprint(ch)) {
            break;
        }
        out[i] = (char)tolower(ch);
    }
    out[i] = '\0';
}

static void probe_result_init(PROBE_RESULT *result)
{
    if (!result) {
        return;
    }
    memset(result, 0, sizeof(*result));
}

static void probe_result_free(PROBE_RESULT *result)
{
    if (!result) {
        return;
    }
    free(result->video_streams);
    result->video_streams = NULL;
    result->video_stream_count = 0;
    free(result->pg_streams);
    result->pg_streams = NULL;
    result->pg_stream_count = 0;
}

static void probe_result_set_source(PROBE_RESULT *result, const char *source)
{
    if (!result || !source) {
        return;
    }
    snprintf(result->source, sizeof(result->source), "%s", source);
}

static void probe_result_set_clip(PROBE_RESULT *result, const char *clip_id, uint32_t clip_ref)
{
    if (!result || !clip_id || !*clip_id) {
        return;
    }
    memset(result->clip_id, 0, sizeof(result->clip_id));
    memcpy(result->clip_id, clip_id, 5);
    result->clip_ref = clip_ref;
}

static void probe_result_set_clip_duration(PROBE_RESULT *result, uint32_t clip_duration_ticks)
{
    if (!result) {
        return;
    }
    result->clip_duration_ticks = clip_duration_ticks;
}

static void probe_result_set_clip_times(PROBE_RESULT *result,
                                        uint32_t clip_in_time_ticks,
                                        uint32_t clip_out_time_ticks)
{
    if (!result) {
        return;
    }
    result->clip_in_time_ticks = clip_in_time_ticks;
    result->clip_out_time_ticks = clip_out_time_ticks;
}

static void probe_result_set_bitrate_mode(PROBE_RESULT *result, const char *mode)
{
    if (!result || !mode) {
        return;
    }
    snprintf(result->bitrate_mode, sizeof(result->bitrate_mode), "%s", mode);
}

static int probe_result_add_video_stream(PROBE_RESULT *result, uint16_t pid)
{
    uint16_t *streams;
    size_t i;

    if (!result || pid == 0) {
        return 0;
    }

    for (i = 0; i < result->video_stream_count; i++) {
        if (result->video_streams[i] == pid) {
            return 1;
        }
    }

    streams = (uint16_t *)realloc(result->video_streams,
                                  sizeof(uint16_t) * (result->video_stream_count + 1));
    if (!streams) {
        return 0;
    }
    result->video_streams = streams;
    result->video_streams[result->video_stream_count++] = pid;
    return 1;
}

static int probe_result_upsert_pg_stream(PROBE_RESULT *result,
                                         uint16_t pid,
                                         const char *lang,
                                         uint8_t coding_type,
                                         uint8_t char_code,
                                         uint8_t subpath_id,
                                         int prefer_new_lang)
{
    PG_STREAM_INFO *streams;
    size_t i;

    if (!result || pid == 0) {
        return 0;
    }

    for (i = 0; i < result->pg_stream_count; i++) {
        PG_STREAM_INFO *stream = &result->pg_streams[i];
        if (stream->pid != pid) {
            continue;
        }

        if (lang_is_present(lang) &&
            (prefer_new_lang || !lang_is_present(stream->lang))) {
            snprintf(stream->lang, sizeof(stream->lang), "%s", lang);
        }
        if (coding_type != 0 &&
            (prefer_new_lang || stream->coding_type == 0)) {
            stream->coding_type = coding_type;
        }
        if (char_code != 0 &&
            (prefer_new_lang || stream->char_code == 0)) {
            stream->char_code = char_code;
        }
        if (subpath_id != 0 &&
            (prefer_new_lang || stream->subpath_id == 0)) {
            stream->subpath_id = subpath_id;
        }
        return 1;
    }

    streams = (PG_STREAM_INFO *)realloc(
        result->pg_streams,
        sizeof(PG_STREAM_INFO) * (result->pg_stream_count + 1));
    if (!streams) {
        return 0;
    }

    result->pg_streams = streams;
    memset(&result->pg_streams[result->pg_stream_count], 0, sizeof(PG_STREAM_INFO));
    result->pg_streams[result->pg_stream_count].pid = pid;
    result->pg_streams[result->pg_stream_count].coding_type = coding_type;
    result->pg_streams[result->pg_stream_count].char_code = char_code;
    result->pg_streams[result->pg_stream_count].subpath_id = subpath_id;
    if (lang_is_present(lang)) {
        snprintf(result->pg_streams[result->pg_stream_count].lang,
                 sizeof(result->pg_streams[result->pg_stream_count].lang),
                 "%s",
                 lang);
    }
    result->pg_stream_count++;
    return 1;
}

static PG_STREAM_INFO *probe_result_find_pg_stream(PROBE_RESULT *result, uint16_t pid)
{
    size_t i;

    if (!result || pid == 0) {
        return NULL;
    }

    for (i = 0; i < result->pg_stream_count; i++) {
        if (result->pg_streams[i].pid == pid) {
            return &result->pg_streams[i];
        }
    }
    return NULL;
}

static void probe_result_finalize_pg_bitrates(PROBE_RESULT *result)
{
    size_t i;

    if (!result || !(result->clip_packet_seconds > 0.0)) {
        return;
    }

    for (i = 0; i < result->pg_stream_count; i++) {
        PG_STREAM_INFO *stream = &result->pg_streams[i];
        if (stream->payload_bytes == 0) {
            stream->bitrate = 0;
            continue;
        }
        stream->bitrate = round_to_u64(
            ((double)stream->payload_bytes * 8.0) / result->clip_packet_seconds);
    }
}

static char *build_disc_file_path(const char *disc_path,
                                  const char *subdir,
                                  const char *name,
                                  const char *ext)
{
    char *path;
    size_t length;

    if (!disc_path || !subdir || !name || !ext) {
        return NULL;
    }

    length = strlen(disc_path) + strlen("/BDMV/") +
             strlen(subdir) + 1 +
             strlen(name) + strlen(ext) + 1;
    path = (char *)malloc(length);
    if (!path) {
        return NULL;
    }

    snprintf(path, length, "%s/BDMV/%s/%s%s", disc_path, subdir, name, ext);
    return path;
}

static int read_file_bytes(const char *path, uint8_t **out_data, size_t *out_size)
{
    FILE *file = NULL;
    long size_long;
    size_t read_size;
    uint8_t *data = NULL;

    if (!path || !out_data || !out_size) {
        return 0;
    }

    file = fopen(path, "rb");
    if (!file) {
        return 0;
    }

    if (fseek(file, 0, SEEK_END) != 0) {
        fclose(file);
        return 0;
    }
    size_long = ftell(file);
    if (size_long < 0) {
        fclose(file);
        return 0;
    }
    if (fseek(file, 0, SEEK_SET) != 0) {
        fclose(file);
        return 0;
    }

    data = (uint8_t *)malloc((size_t)size_long);
    if (!data) {
        fclose(file);
        return 0;
    }

    read_size = fread(data, 1, (size_t)size_long, file);
    fclose(file);

    if (read_size != (size_t)size_long) {
        free(data);
        return 0;
    }

    *out_data = data;
    *out_size = read_size;
    return 1;
}

static int file_type_matches(const uint8_t *data,
                             size_t size,
                             const char *type1,
                             const char *type2,
                             const char *type3)
{
    if (!data || size < 8) {
        return 0;
    }
    return memcmp(data, type1, 8) == 0 ||
           memcmp(data, type2, 8) == 0 ||
           memcmp(data, type3, 8) == 0;
}

static int clip_id_matches(const char *clip_id, const char *requested_clip)
{
    if (!clip_id || !*clip_id) {
        return 0;
    }
    if (!requested_clip || !*requested_clip) {
        return 1;
    }
    return strncmp(clip_id, requested_clip, 5) == 0;
}

static int parse_playlist_stream_descriptor(const uint8_t *data,
                                           size_t size,
                                           size_t *pos,
                                           PROBE_RESULT *result)
{
    size_t header_pos;
    size_t stream_pos;
    size_t next_pos;
    uint8_t header_length;
    uint8_t header_type;
    uint8_t stream_length;
    uint8_t stream_type;
    uint16_t pid = 0;
    uint8_t subpath_id = 0;
    uint8_t char_code = 0;
    char lang[4] = {0};

    if (!data || !pos || !result || *pos + 2 > size) {
        return 0;
    }

    header_length = data[(*pos)++];
    header_pos = *pos;
    header_type = data[(*pos)++];

    switch (header_type) {
    case 1:
        if (*pos + 2 > size) {
            return 0;
        }
        pid = read_be16(data, *pos);
        *pos += 2;
        break;
    case 2:
    case 4:
        if (*pos + 4 > size) {
            return 0;
        }
        subpath_id = data[(*pos)++];
        *pos += 1;
        pid = read_be16(data, *pos);
        *pos += 2;
        break;
    case 3:
        if (*pos + 3 > size) {
            return 0;
        }
        subpath_id = data[(*pos)++];
        pid = read_be16(data, *pos);
        *pos += 2;
        break;
    default:
        break;
    }

    next_pos = header_pos + (size_t)header_length;
    if (next_pos > size || next_pos + 2 > size) {
        return 0;
    }
    *pos = next_pos;

    stream_length = data[(*pos)++];
    stream_pos = *pos;
    if (*pos >= size) {
        return 0;
    }

    stream_type = data[(*pos)++];
    if (stream_type_is_video(stream_type)) {
        if (!probe_result_add_video_stream(result, pid)) {
            return 0;
        }
    } else if ((stream_type == STREAM_TYPE_PRESENTATION_GRAPHICS ||
                stream_type == STREAM_TYPE_INTERACTIVE_GRAPHICS) &&
               *pos + 3 <= size) {
        normalize_lang_code(&data[*pos], lang);
        if (*pos + 3 < size) {
            char_code = data[*pos + 3];
        }
        if (!probe_result_upsert_pg_stream(result,
                                           pid,
                                           lang,
                                           stream_type,
                                           char_code,
                                           subpath_id,
                                           1)) {
            return 0;
        }
    }

    next_pos = stream_pos + (size_t)stream_length;
    if (next_pos > size) {
        return 0;
    }
    *pos = next_pos;
    return 1;
}

static int parse_mpls_streams(const char *disc_path,
                              uint32_t playlist,
                              const char *requested_clip,
                              PROBE_RESULT *result)
{
    char playlist_name[16];
    char item_name[6];
    char *path = NULL;
    uint8_t *data = NULL;
    size_t size = 0;
    size_t pos;
    size_t item_end;
    size_t item_start;
    uint32_t playlist_offset;
    uint16_t item_count;
    int found_any_clip = 0;
    int added_any_stream = 0;
    uint16_t item_index;

    if (!disc_path || !result) {
        return 0;
    }

    snprintf(playlist_name, sizeof(playlist_name), "%05u", playlist);
    path = build_disc_file_path(disc_path, "PLAYLIST", playlist_name, ".mpls");
    if (!path) {
        return 0;
    }

    if (!read_file_bytes(path, &data, &size) ||
        !file_type_matches(data, size, "MPLS0100", "MPLS0200", "MPLS0300") ||
        size < 20) {
        free(path);
        free(data);
        return 0;
    }
    free(path);

    pos = 8;
    playlist_offset = read_be32(data, pos);
    if ((size_t)playlist_offset + 10 > size) {
        free(data);
        return 0;
    }

    pos = playlist_offset;
    pos += 4;
    pos += 2;
    item_count = read_be16(data, pos);
    pos += 2;
    pos += 2;

    for (item_index = 0; item_index < item_count; item_index++) {
        uint16_t item_length;
        uint8_t multiangle;
        uint8_t stream_count_video;
        uint8_t stream_count_audio;
        uint8_t stream_count_pg;
        uint32_t in_time;
        uint32_t out_time;
        uint32_t clip_duration_ticks;
        uint16_t count_index;
        int clip_match;

        if (pos + 2 > size) {
            free(data);
            return 0;
        }

        item_start = pos;
        item_length = read_be16(data, pos);
        item_end = item_start + 2u + item_length;
        if (item_end > size || item_end < item_start) {
            free(data);
            return 0;
        }
        pos += 2;

        if (pos + 9 > item_end) {
            free(data);
            return 0;
        }
        memcpy(item_name, &data[pos], 5);
        item_name[5] = '\0';
        pos += 5;
        pos += 4;

        pos += 1;
        if (pos + 1 > item_end) {
            free(data);
            return 0;
        }
        multiangle = (data[pos] >> 4) & 0x01;
        pos += 2;

        if (pos + 8 > item_end) {
            free(data);
            return 0;
        }
        in_time = read_be32(data, pos);
        if ((in_time & 0x80000000u) != 0) {
            in_time &= 0x7fffffffu;
        }
        pos += 4;
        out_time = read_be32(data, pos);
        if ((out_time & 0x80000000u) != 0) {
            out_time &= 0x7fffffffu;
        }
        pos += 4;
        clip_duration_ticks = out_time > in_time ? out_time - in_time : 0;

        if (pos + 12 > item_end) {
            free(data);
            return 0;
        }
        pos += 12;

        if (multiangle > 0) {
            uint8_t angles;
            uint8_t angle;

            if (pos + 2 > item_end) {
                free(data);
                return 0;
            }
            angles = data[pos];
            pos += 2;
            for (angle = 0; angle + 1 < angles; angle++) {
                if (pos + 10 > item_end) {
                    free(data);
                    return 0;
                }
                pos += 10;
            }
        }

        if (pos + 16 > item_end) {
            free(data);
            return 0;
        }
        pos += 2;
        pos += 2;
        stream_count_video = data[pos++];
        stream_count_audio = data[pos++];
        stream_count_pg = data[pos++];
        pos += 1;
        pos += 1;
        pos += 1;
        pos += 1;
        pos += 5;

        clip_match = clip_id_matches(item_name, requested_clip);
        if (clip_match) {
            size_t before_count = result->pg_stream_count;

            found_any_clip = 1;
            probe_result_set_clip(result, item_name, item_index);
            probe_result_set_clip_times(result, in_time, out_time);
            probe_result_set_clip_duration(result, clip_duration_ticks);

            for (count_index = 0; count_index < stream_count_video; count_index++) {
                if (!parse_playlist_stream_descriptor(data, item_end, &pos, result)) {
                    free(data);
                    return 0;
                }
            }
            for (count_index = 0; count_index < stream_count_audio; count_index++) {
                if (!parse_playlist_stream_descriptor(data, item_end, &pos, result)) {
                    free(data);
                    return 0;
                }
            }
            for (count_index = 0; count_index < stream_count_pg; count_index++) {
                if (!parse_playlist_stream_descriptor(data, item_end, &pos, result)) {
                    free(data);
                    return 0;
                }
            }

            if (result->pg_stream_count > before_count) {
                added_any_stream = 1;
            }
        }

        pos = item_end;
    }

    free(data);
    if (!found_any_clip) {
        return 0;
    }
    return added_any_stream || result->pg_stream_count > 0;
}

static int parse_clpi_streams(const char *disc_path,
                              const char *requested_clip,
                              PROBE_RESULT *result)
{
    char *path = NULL;
    uint8_t *data = NULL;
    size_t size = 0;
    size_t clip_offset;
    size_t clip_size;
    size_t stream_offset;
    size_t stream_index;
    int found_pg = 0;
    const char *clip_id = requested_clip;

    if (!disc_path || !result) {
        return 0;
    }

    if ((!clip_id || !*clip_id) && result->clip_id[0] != '\0') {
        clip_id = result->clip_id;
    }
    if (!clip_id || !*clip_id) {
        return 0;
    }

    path = build_disc_file_path(disc_path, "CLIPINF", clip_id, ".clpi");
    if (!path) {
        return 0;
    }

    if (!read_file_bytes(path, &data, &size) ||
        !file_type_matches(data, size, "HDMV0100", "HDMV0200", "HDMV0300") ||
        size < 16) {
        free(path);
        free(data);
        return 0;
    }
    free(path);

    clip_offset = read_be32(data, 12);
    if (clip_offset + 4 > size) {
        free(data);
        return 0;
    }

    clip_size = read_be32(data, clip_offset);
    clip_offset += 4;
    if (clip_offset + clip_size > size || clip_size < 10) {
        free(data);
        return 0;
    }

    stream_offset = clip_offset + 10;
    for (stream_index = 0; stream_index < data[clip_offset + 8]; stream_index++) {
        size_t descriptor_start;
        size_t descriptor_next;
        uint16_t pid;
        uint8_t descriptor_length;
        uint8_t stream_type;
        char lang[4] = {0};

        if (stream_offset + 4 > clip_offset + clip_size) {
            free(data);
            return 0;
        }

        pid = read_be16(data, stream_offset);
        descriptor_start = stream_offset + 2;
        descriptor_length = data[descriptor_start];
        descriptor_next = descriptor_start + (size_t)descriptor_length + 1;
        if (descriptor_next > clip_offset + clip_size ||
            descriptor_start + 2 > clip_offset + clip_size) {
            free(data);
            return 0;
        }

        stream_type = data[descriptor_start + 1];
        if (stream_type_is_video(stream_type)) {
            if (!probe_result_add_video_stream(result, pid)) {
                free(data);
                return 0;
            }
        } else if ((stream_type == STREAM_TYPE_PRESENTATION_GRAPHICS ||
                    stream_type == STREAM_TYPE_INTERACTIVE_GRAPHICS) &&
                   descriptor_start + 5 <= clip_offset + clip_size) {
            normalize_lang_code(&data[descriptor_start + 2], lang);
            if (!probe_result_upsert_pg_stream(result,
                                               pid,
                                               lang,
                                               stream_type,
                                               0,
                                               0,
                                               0)) {
                free(data);
                return 0;
            }
            found_pg = 1;
        }

        stream_offset = descriptor_next;
    }

    probe_result_set_clip(result, clip_id, result->clip_ref);
    free(data);
    return found_pg;
}

static SCAN_PID_STATE *find_scan_state(SCAN_PID_STATE *states, size_t count, uint16_t pid)
{
    size_t i;

    if (!states || pid == 0) {
        return NULL;
    }
    for (i = 0; i < count; i++) {
        if (states[i].pid == pid) {
            return &states[i];
        }
    }
    return NULL;
}

static void probe_result_update_clip_packet_seconds(PROBE_RESULT *result,
                                                    double stream_time,
                                                    double stream_interval)
{
    double clip_in;
    double clip_out;
    double stream_offset;

    if (!result) {
        return;
    }

    clip_in = (double)result->clip_in_time_ticks / 45000.0;
    clip_out = (double)result->clip_out_time_ticks / 45000.0;
    stream_offset = stream_time + stream_interval;

    if (stream_time != 0.0 &&
        (stream_time < clip_in || stream_time > clip_out)) {
        return;
    }
    if (stream_offset > clip_in &&
        stream_offset - clip_in > result->clip_packet_seconds) {
        result->clip_packet_seconds = stream_offset - clip_in;
    }
}

static int scan_state_header_matches(const SCAN_PID_STATE *state)
{
    if (!state) {
        return 0;
    }

    if (state->kind == SCAN_KIND_VIDEO) {
        return state->parse == 0x000001FDu ||
               (state->parse >= 0x000001E0u && state->parse <= 0x000001EFu);
    }

    return state->parse == 0x000001FAu ||
           state->parse == 0x000001FDu ||
           state->parse == 0x000001BDu ||
           (state->parse >= 0x000001E0u && state->parse <= 0x000001EFu);
}

static void scan_state_process_timestamp(PROBE_RESULT *result,
                                         SCAN_PID_STATE *state,
                                         uint64_t current_dts)
{
    double stream_time;
    double stream_interval;

    if (!result || !state || state->kind != SCAN_KIND_VIDEO) {
        return;
    }

    stream_time = (double)current_dts / 90000.0;
    stream_interval = (double)(current_dts - state->dts_prev) / 90000.0;

    if (state->pts_count > 0) {
        probe_result_update_clip_packet_seconds(result, stream_time, stream_interval);
    }

    state->dts_prev = current_dts;
    state->pts_count++;
}

static void scan_state_process_payload_byte(PROBE_RESULT *result,
                                            SCAN_PID_STATE *state,
                                            uint8_t value)
{
    if (!state) {
        return;
    }

    state->parse = (state->parse << 8) | (uint32_t)value;

    if (state->transfer_state) {
        if (state->kind == SCAN_KIND_GRAPHICS && state->pg_stream) {
            state->pg_stream->payload_bytes++;
        }
        if (!state->packet_length_variable) {
            if (state->packet_length > 0) {
                state->packet_length--;
            }
            if (state->packet_length <= 0) {
                state->packet_length = 0;
                state->transfer_state = 0;
            }
        }
        return;
    }

    if (scan_state_header_matches(state)) {
        state->packet_length_parse = 2;
        state->packet_length = 0;
        state->packet_length_variable = 0;
        return;
    }

    if (state->packet_length_parse > 0) {
        state->packet_length_parse--;
        switch (state->packet_length_parse) {
        case 1:
            state->packet_length = ((int)(state->parse & 0xFFu)) << 8;
            break;
        case 0:
            state->packet_length |= (int)(state->parse & 0xFFu);
            if (state->packet_length == 0) {
                state->packet_length_variable = 1;
            }
            state->packet_parse = 3;
            break;
        default:
            break;
        }
        return;
    }

    if (state->packet_parse > 0) {
        if (!state->packet_length_variable && state->packet_length > 0) {
            state->packet_length--;
        }
        state->packet_parse--;
        switch (state->packet_parse) {
        case 1:
            state->pes_header_flags = (uint8_t)(state->parse & 0xFFu);
            break;
        case 0:
            state->pes_header_length = (uint8_t)(state->parse & 0xFFu);
            if ((state->pes_header_flags & 0xC0u) == 0x80u) {
                state->pts_parse = 5;
            } else if ((state->pes_header_flags & 0xC0u) == 0xC0u) {
                state->dts_parse = 10;
            } else if (state->pes_header_length == 0) {
                state->transfer_state = 1;
            }
            break;
        default:
            break;
        }
        return;
    }

    if (state->pts_parse > 0) {
        if (!state->packet_length_variable && state->packet_length > 0) {
            state->packet_length--;
        }
        if (state->pes_header_length > 0) {
            state->pes_header_length--;
        }
        state->pts_parse--;

        switch (state->pts_parse) {
        case 4:
            state->pts_temp = (uint64_t)(state->parse & 0x0Eu) << 29;
            break;
        case 3:
            state->pts_temp |= (uint64_t)(state->parse & 0xFFu) << 22;
            break;
        case 2:
            state->pts_temp |= (uint64_t)(state->parse & 0xFEu) << 14;
            break;
        case 1:
            state->pts_temp |= (uint64_t)(state->parse & 0xFFu) << 7;
            break;
        case 0:
            state->pts_temp |= (uint64_t)((state->parse & 0xFEu) >> 1);
            state->pts_last = state->pts_temp;
            if (state->kind == SCAN_KIND_VIDEO) {
                scan_state_process_timestamp(result, state, state->pts_temp);
            }
            if (state->pes_header_length == 0 && state->dts_parse == 0) {
                state->transfer_state = 1;
            }
            break;
        default:
            break;
        }
        return;
    }

    if (state->dts_parse > 0) {
        if (!state->packet_length_variable && state->packet_length > 0) {
            state->packet_length--;
        }
        if (state->pes_header_length > 0) {
            state->pes_header_length--;
        }
        state->dts_parse--;

        switch (state->dts_parse) {
        case 9:
            state->pts_temp = (uint64_t)(state->parse & 0x0Eu) << 29;
            break;
        case 8:
            state->pts_temp |= (uint64_t)(state->parse & 0xFFu) << 22;
            break;
        case 7:
            state->pts_temp |= (uint64_t)(state->parse & 0xFEu) << 14;
            break;
        case 6:
            state->pts_temp |= (uint64_t)(state->parse & 0xFFu) << 7;
            break;
        case 5:
            state->pts_temp |= (uint64_t)((state->parse & 0xFEu) >> 1);
            state->pts_last = state->pts_temp;
            break;
        case 4:
            state->dts_temp = (uint64_t)(state->parse & 0x0Eu) << 29;
            break;
        case 3:
            state->dts_temp |= (uint64_t)(state->parse & 0xFFu) << 22;
            break;
        case 2:
            state->dts_temp |= (uint64_t)(state->parse & 0xFEu) << 14;
            break;
        case 1:
            state->dts_temp |= (uint64_t)(state->parse & 0xFFu) << 7;
            break;
        case 0:
            state->dts_temp |= (uint64_t)((state->parse & 0xFEu) >> 1);
            if (state->kind == SCAN_KIND_VIDEO) {
                scan_state_process_timestamp(result, state, state->dts_temp);
            }
            if (state->pes_header_length == 0) {
                state->transfer_state = 1;
            }
            break;
        default:
            break;
        }
        return;
    }

    if (state->pes_header_length > 0) {
        if (!state->packet_length_variable && state->packet_length > 0) {
            state->packet_length--;
        }
        state->pes_header_length--;
        if (state->pes_header_length == 0) {
            state->transfer_state = 1;
        }
    }
}

static SCAN_PID_STATE *probe_result_build_scan_states(PROBE_RESULT *result, size_t *out_count)
{
    SCAN_PID_STATE *states;
    size_t count;
    size_t index = 0;
    size_t i;

    if (out_count) {
        *out_count = 0;
    }
    if (!result) {
        return NULL;
    }

    count = result->video_stream_count + result->pg_stream_count;
    if (count == 0) {
        return NULL;
    }

    states = (SCAN_PID_STATE *)calloc(count, sizeof(SCAN_PID_STATE));
    if (!states) {
        return NULL;
    }

    for (i = 0; i < result->video_stream_count; i++) {
        states[index].pid = result->video_streams[i];
        states[index].kind = SCAN_KIND_VIDEO;
        index++;
    }
    for (i = 0; i < result->pg_stream_count; i++) {
        states[index].pid = result->pg_streams[i].pid;
        states[index].kind = SCAN_KIND_GRAPHICS;
        states[index].pg_stream = &result->pg_streams[i];
        index++;
    }

    if (out_count) {
        *out_count = count;
    }
    return states;
}

static size_t parse_m2ts_packet_payload_size(const uint8_t *packet, uint16_t *out_pid)
{
    const uint8_t *ts;
    uint8_t adaptation_control;
    size_t payload_pos = 4;

    if (out_pid) {
        *out_pid = 0;
    }
    if (!packet || packet[M2TS_TS_SYNC_OFFSET] != 0x47) {
        return 0;
    }

    ts = packet + M2TS_TS_SYNC_OFFSET;
    if (out_pid) {
        *out_pid = (uint16_t)(((uint16_t)(ts[1] & 0x1f) << 8) | ts[2]);
    }

    adaptation_control = (uint8_t)((ts[3] >> 4) & 0x03);
    if (adaptation_control == 0 || adaptation_control == 2) {
        return 0;
    }
    if (adaptation_control == 3) {
        payload_pos += 1u + (size_t)ts[payload_pos];
        if (payload_pos > TS_PACKET_SIZE) {
            return 0;
        }
    }
    if (payload_pos >= TS_PACKET_SIZE) {
        return 0;
    }
    return TS_PACKET_SIZE - payload_pos;
}

static int scan_m2ts_pg_payload_window(FILE *file,
                                       uint8_t *buffer,
                                       size_t buffer_size,
                                       uint64_t start_byte,
                                       uint64_t window_bytes,
                                       PROBE_RESULT *result)
{
    uint64_t remaining = window_bytes;

    if (!file || !buffer || buffer_size < M2TS_PACKET_SIZE || !result) {
        return 0;
    }
    if (window_bytes == 0) {
        return 1;
    }
    if (fseeko(file, (off_t)start_byte, SEEK_SET) != 0) {
        return 0;
    }

    while (remaining > 0) {
        size_t offset = 0;
        size_t to_read = buffer_size;
        if ((uint64_t)to_read > remaining) {
            to_read = (size_t)remaining;
        }
        to_read -= to_read % M2TS_PACKET_SIZE;
        if (to_read == 0) {
            break;
        }

        {
            size_t read_size = fread(buffer, 1, to_read, file);
            if (read_size == 0) {
                return feof(file) ? 1 : 0;
            }
            while (offset + M2TS_PACKET_SIZE <= read_size) {
                uint16_t pid = 0;
                size_t payload_size = parse_m2ts_packet_payload_size(buffer + offset, &pid);
                PG_STREAM_INFO *stream = probe_result_find_pg_stream(result, pid);
                if (stream && payload_size > 0) {
                    stream->payload_bytes += (uint64_t)payload_size;
                }
                offset += M2TS_PACKET_SIZE;
            }
            remaining -= (uint64_t)offset;
            if (offset == 0) {
                break;
            }
            if (read_size < to_read) {
                break;
            }
        }
    }

    return ferror(file) == 0;
}

static int probe_result_scan_clip_pg_payload_bytes(const char *disc_path, PROBE_RESULT *result)
{
    char *path = NULL;
    uint8_t *buffer = NULL;
    FILE *file = NULL;
    off_t file_size_off = 0;
    uint64_t file_size = 0;
    uint64_t total_packets = 0;
    uint64_t window_packets = 0;
    uint64_t sampled_bytes = 0;
    unsigned int window_count = 0;
    unsigned int window_index;
    size_t i;
    int success = 0;

    if (!disc_path || !result || result->clip_id[0] == '\0' || result->pg_stream_count == 0) {
        return 0;
    }

    for (i = 0; i < result->pg_stream_count; i++) {
        result->pg_streams[i].payload_bytes = 0;
        result->pg_streams[i].bitrate = 0;
    }
    result->bitrate_scanned = 0;
    result->clip_packet_seconds = 0.0;
    probe_result_set_bitrate_mode(result, "");

    path = build_disc_file_path(disc_path, "STREAM", result->clip_id, ".m2ts");
    if (!path) {
        return 0;
    }

    log_stagef("start payload scan: playlist=%05u clip=%s mode=payload-bytes",
               result->playlist,
               result->clip_id);
    log_stagef("scan target: %s", path);

    file = fopen(path, "rb");
    if (!file) {
        log_stagef("payload scan failed: unable to open %s", path);
        free(path);
        return 0;
    }

    buffer = (uint8_t *)malloc(M2TS_READ_CHUNK + M2TS_PACKET_SIZE);
    if (!buffer) {
        fclose(file);
        free(path);
        return 0;
    }

    if (fseeko(file, 0, SEEK_END) != 0) {
        free(buffer);
        fclose(file);
        free(path);
        return 0;
    }
    file_size_off = ftello(file);
    if (file_size_off <= 0) {
        free(buffer);
        fclose(file);
        free(path);
        return 0;
    }
    file_size = (uint64_t)file_size_off;
    if (file_size < M2TS_PACKET_SIZE) {
        free(buffer);
        fclose(file);
        free(path);
        return 0;
    }
    total_packets = file_size / M2TS_PACKET_SIZE;
    if (total_packets == 0) {
        free(buffer);
        fclose(file);
        free(path);
        return 0;
    }

    window_count = M2TS_SAMPLE_WINDOW_COUNT;
    if ((uint64_t)window_count > total_packets) {
        window_count = (unsigned int)total_packets;
    }
    if (window_count == 0) {
        window_count = 1;
    }

    window_packets = (uint64_t)(M2TS_SAMPLE_WINDOW_BYTES / M2TS_PACKET_SIZE);
    if (window_packets == 0) {
        window_packets = 1;
    }

    {
        uint64_t segment_packets = total_packets / (uint64_t)window_count;
        uint64_t max_window_packets = segment_packets / 4u;
        if (max_window_packets == 0) {
            max_window_packets = 1;
        }
        if (window_packets > max_window_packets) {
            window_packets = max_window_packets;
        }
    }
    sampled_bytes = window_packets * M2TS_PACKET_SIZE * (uint64_t)window_count;

    setvbuf(file, NULL, _IOFBF, M2TS_READ_CHUNK);
    log_stagef("payload sample plan: windows=%u window_bytes=%" PRIu64 " sampled_bytes=%" PRIu64 " file_size=%" PRIu64,
               window_count,
               window_packets * M2TS_PACKET_SIZE,
               sampled_bytes,
               file_size);

    for (window_index = 0; window_index < window_count; window_index++) {
        uint64_t segment_start = (total_packets * (uint64_t)window_index) / (uint64_t)window_count;
        uint64_t segment_end = (total_packets * (uint64_t)(window_index + 1)) / (uint64_t)window_count;
        uint64_t segment_packets = segment_end > segment_start ? segment_end - segment_start : 0;
        uint64_t current_window_packets = window_packets;
        uint64_t start_packet;

        if (segment_packets == 0) {
            continue;
        }
        if (current_window_packets > segment_packets) {
            current_window_packets = segment_packets;
        }
        if (current_window_packets == 0) {
            current_window_packets = 1;
        }
        start_packet = segment_start + (segment_packets - current_window_packets) / 2u;

        if (!scan_m2ts_pg_payload_window(file,
                                         buffer,
                                         M2TS_READ_CHUNK,
                                         start_packet * M2TS_PACKET_SIZE,
                                         current_window_packets * M2TS_PACKET_SIZE,
                                         result)) {
            free(buffer);
            fclose(file);
            free(path);
            return 0;
        }
    }

    if (ferror(file) == 0) {
        probe_result_set_bitrate_mode(result, "sampled-payload-bytes");
        log_stagef("finish payload scan: clip=%s pg_streams=%zu windows=%u",
                   result->clip_id,
                   result->pg_stream_count,
                   window_count);
        success = 1;
    }

    free(buffer);
    fclose(file);
    free(path);
    return success;
}

static int probe_result_scan_clip_pg_bitrates(const char *disc_path, PROBE_RESULT *result)
{
    char *path = NULL;
    uint8_t *buffer = NULL;
    FILE *file = NULL;
    SCAN_PID_STATE *states = NULL;
    size_t state_count = 0;
    size_t carry = 0;
    size_t i;
    int success = 0;

    if (!disc_path || !result || result->clip_id[0] == '\0' || result->pg_stream_count == 0) {
        return 0;
    }
    if (result->video_stream_count == 0) {
        log_stagef("bdsub bitrate scan skipped: no video PID found for clip %s", result->clip_id);
        return 0;
    }

    for (i = 0; i < result->pg_stream_count; i++) {
        result->pg_streams[i].payload_bytes = 0;
        result->pg_streams[i].bitrate = 0;
    }
    result->clip_packet_seconds = 0.0;
    result->bitrate_scanned = 0;
    probe_result_set_bitrate_mode(result, "");

    states = probe_result_build_scan_states(result, &state_count);
    if (!states) {
        return 0;
    }

    path = build_disc_file_path(disc_path, "STREAM", result->clip_id, ".m2ts");
    if (!path) {
        free(states);
        return 0;
    }

    log_stagef("start bitrate scan: playlist=%05u clip=%s mode=bdinfo-tsstreamfile",
               result->playlist,
               result->clip_id);
    log_stagef("scan target: %s", path);

    file = fopen(path, "rb");
    if (!file) {
        log_stagef("bitrate scan failed: unable to open %s", path);
        free(path);
        free(states);
        return 0;
    }

    buffer = (uint8_t *)malloc(M2TS_READ_CHUNK + M2TS_PACKET_SIZE);
    if (!buffer) {
        fclose(file);
        free(path);
        free(states);
        return 0;
    }

    setvbuf(file, NULL, _IOFBF, M2TS_READ_CHUNK);
    while (!feof(file)) {
        size_t read_size = fread(buffer + carry, 1, M2TS_READ_CHUNK, file);
        size_t total = carry + read_size;
        size_t offset = 0;

        while (offset + M2TS_PACKET_SIZE <= total) {
            const uint8_t *packet = buffer + offset;
            const uint8_t *ts;
            SCAN_PID_STATE *state;
            uint16_t pid;
            uint8_t adaptation_control;
            uint8_t payload_unit_start;
            size_t payload_pos = 4;
            size_t payload_index;

            if (packet[M2TS_TS_SYNC_OFFSET] != 0x47) {
                log_stagef("bitrate scan failed: invalid sync byte in %s", path);
                goto cleanup;
            }

            ts = packet + M2TS_TS_SYNC_OFFSET;
            pid = (uint16_t)(((uint16_t)(ts[1] & 0x1f) << 8) | ts[2]);
            state = find_scan_state(states, state_count, pid);
            if (!state) {
                offset += M2TS_PACKET_SIZE;
                continue;
            }

            adaptation_control = (uint8_t)((ts[3] >> 4) & 0x03);
            payload_unit_start = (uint8_t)((ts[1] >> 6) & 0x01);
            if (adaptation_control == 0 || adaptation_control == 2) {
                offset += M2TS_PACKET_SIZE;
                continue;
            }
            if (adaptation_control == 3) {
                payload_pos += 1u + (size_t)ts[payload_pos];
                if (payload_pos > TS_PACKET_SIZE) {
                    offset += M2TS_PACKET_SIZE;
                    continue;
                }
            }
            if (payload_pos >= TS_PACKET_SIZE) {
                offset += M2TS_PACKET_SIZE;
                continue;
            }

            if (payload_unit_start && state->transfer_state) {
                state->transfer_state = 0;
            }
            if (payload_unit_start) {
                state->packet_length = 0;
                state->packet_length_variable = 0;
                state->packet_length_parse = 0;
                state->packet_parse = 0;
                state->pes_header_length = 0;
                state->pes_header_flags = 0;
                state->pts_parse = 0;
                state->pts_temp = 0;
                state->dts_parse = 0;
                state->dts_temp = 0;
                state->parse = 0;
            }

            for (payload_index = payload_pos; payload_index < TS_PACKET_SIZE; payload_index++) {
                scan_state_process_payload_byte(result, state, ts[payload_index]);
            }

            offset += M2TS_PACKET_SIZE;
        }

        carry = total - offset;
        if (carry > 0) {
            memmove(buffer, buffer + offset, carry);
        }
    }

    if (ferror(file) == 0) {
        probe_result_finalize_pg_bitrates(result);
        result->bitrate_scanned = 1;
        probe_result_set_bitrate_mode(result, "bdinfo-tsstreamfile");
        log_stagef("finish bitrate scan: clip=%s packet_seconds=%.3f pg_streams=%zu",
                   result->clip_id,
                   result->clip_packet_seconds,
                   result->pg_stream_count);
        success = 1;
    }

cleanup:
    free(buffer);
    fclose(file);
    free(path);
    free(states);
    return success;
}

static void print_probe_result_json(const char *disc_path, const PROBE_RESULT *result)
{
    size_t i;

    (void)disc_path;
    printf("{");
    printf("\"source\":");
    json_print_string(result && result->source[0] ? result->source : "unknown");
    printf(",\"bitrate_scanned\":%s", result && result->bitrate_scanned ? "true" : "false");
    printf(",\"bitrate_mode\":");
    json_print_string(result && result->bitrate_mode[0] ? result->bitrate_mode : "");
    printf(",\"clip\":{");
    printf("\"clip_id\":");
    json_print_string(result && result->clip_id[0] ? result->clip_id : "");
    printf(",\"pg_stream_count\":%zu", result ? result->pg_stream_count : 0u);
    printf(",\"packet_seconds\":%.3f", result ? result->clip_packet_seconds : 0.0);
    printf(",\"pg_streams\":[");
    if (result) {
        for (i = 0; i < result->pg_stream_count; i++) {
            const PG_STREAM_INFO *stream = &result->pg_streams[i];
            if (i > 0) {
                putchar(',');
            }
            printf("{\"pid\":%u", stream->pid);
            printf(",\"lang\":");
            json_print_string(stream->lang);
            printf(",\"coding_type\":%u", stream->coding_type);
            printf(",\"char_code\":%u", stream->char_code);
            printf(",\"subpath_id\":%u", stream->subpath_id);
            printf(",\"payload_bytes\":%" PRIu64, stream->payload_bytes);
            printf(",\"bitrate\":%" PRIu64, stream->bitrate);
            printf("}");
        }
    }
    printf("]}}");
    putchar('\n');
}

int main(int argc, char **argv)
{
    const char *disc_path = NULL;
    const char *clip_id = NULL;
    PROBE_RESULT result;
    uint32_t playlist = 0;
    int have_playlist = 0;
    int scan_payload = 0;
    int scan_bitrate = 0;
    int argi;
    int mpls_ok;
    int clpi_ok;

    if (argc < 4) {
        usage(argv[0]);
        return 2;
    }

    probe_result_init(&result);
    setvbuf(stderr, NULL, _IONBF, 0);
    disc_path = argv[1];

    for (argi = 2; argi < argc; argi++) {
        if (strcmp(argv[argi], "--playlist") == 0) {
            if (argi + 1 >= argc || !parse_u32(argv[++argi], &playlist)) {
                fprintf(stderr, "bdinfo_style_probe: invalid --playlist value\n");
                probe_result_free(&result);
                return 2;
            }
            have_playlist = 1;
        } else if (strcmp(argv[argi], "--clip") == 0) {
            if (argi + 1 >= argc) {
                fprintf(stderr, "bdinfo_style_probe: missing --clip value\n");
                probe_result_free(&result);
                return 2;
            }
            clip_id = argv[++argi];
        } else if (strcmp(argv[argi], "--scan-payload") == 0) {
            scan_payload = 1;
        } else if (strcmp(argv[argi], "--scan-bitrate") == 0) {
            scan_bitrate = 1;
        } else {
            fprintf(stderr, "bdinfo_style_probe: unknown argument: %s\n", argv[argi]);
            probe_result_free(&result);
            return 2;
        }
    }

    if (!have_playlist) {
        fprintf(stderr, "bdinfo_style_probe: --playlist is required\n");
        probe_result_free(&result);
        return 2;
    }

    result.playlist = playlist;

    mpls_ok = parse_mpls_streams(disc_path, playlist, clip_id, &result);
    clpi_ok = parse_clpi_streams(disc_path, clip_id, &result);

    if (mpls_ok && clpi_ok) {
        probe_result_set_source(&result, "bdinfo-style-mpls+clpi");
    } else if (mpls_ok) {
        probe_result_set_source(&result, "bdinfo-style-mpls");
    } else if (clpi_ok) {
        probe_result_set_source(&result, "bdinfo-style-clpi");
    } else {
        fprintf(stderr,
                "bdinfo_style_probe: no subtitle metadata found for playlist %05u clip %s\n",
                playlist,
                clip_id ? clip_id : "(auto)");
        probe_result_free(&result);
        return 5;
    }

    if (scan_bitrate) {
        if (!probe_result_scan_clip_pg_bitrates(disc_path, &result)) {
            log_stagef("bitrate scan finished without usable packet timing; returning metadata only");
        }
    } else if (scan_payload) {
        if (!probe_result_scan_clip_pg_payload_bytes(disc_path, &result)) {
            log_stagef("payload scan finished without usable stream data; returning metadata only");
        }
    } else {
        probe_result_set_bitrate_mode(&result, "metadata-only");
    }
    print_probe_result_json(disc_path, &result);
    probe_result_free(&result);
    return 0;
}
