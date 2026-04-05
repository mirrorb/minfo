#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define STREAM_TYPE_PRESENTATION_GRAPHICS 0x90
#define STREAM_TYPE_INTERACTIVE_GRAPHICS 0x91
#define M2TS_PACKET_SIZE 192
#define TS_PACKET_SIZE 188
#define M2TS_TS_SYNC_OFFSET 4
#define M2TS_READ_CHUNK (M2TS_PACKET_SIZE * 4096)

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
    uint32_t playlist;
    uint32_t clip_ref;
    uint32_t clip_duration_ticks;
    char clip_id[6];
    size_t pg_stream_count;
    PG_STREAM_INFO *pg_streams;
} PROBE_RESULT;

static void usage(const char *argv0)
{
    fprintf(stderr,
            "usage: %s <disc-path> --playlist <mpls> [--clip <clip-id>]\n",
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

static void probe_result_finalize_pg_bitrates(PROBE_RESULT *result)
{
    size_t i;

    if (!result || result->clip_duration_ticks == 0) {
        return;
    }

    for (i = 0; i < result->pg_stream_count; i++) {
        PG_STREAM_INFO *stream = &result->pg_streams[i];
        if (stream->payload_bytes == 0) {
            stream->bitrate = 0;
            continue;
        }
        stream->bitrate =
            (stream->payload_bytes * 8u * (uint64_t)45000u +
             (uint64_t)result->clip_duration_ticks / 2u) /
            (uint64_t)result->clip_duration_ticks;
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
    if ((stream_type == STREAM_TYPE_PRESENTATION_GRAPHICS ||
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
    int found_any = 0;
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
        if ((stream_type == STREAM_TYPE_PRESENTATION_GRAPHICS ||
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
            found_any = 1;
        }

        stream_offset = descriptor_next;
    }

    probe_result_set_clip(result, clip_id, result->clip_ref);
    free(data);
    return found_any;
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

static int probe_result_scan_clip_pg_bitrates(const char *disc_path, PROBE_RESULT *result)
{
    char *path = NULL;
    uint8_t *buffer = NULL;
    FILE *file = NULL;
    size_t carry = 0;
    int success = 0;

    if (!disc_path || !result || result->clip_id[0] == '\0' || result->pg_stream_count == 0) {
        return 0;
    }

    path = build_disc_file_path(disc_path, "STREAM", result->clip_id, ".m2ts");
    if (!path) {
        return 0;
    }

    file = fopen(path, "rb");
    free(path);
    if (!file) {
        return 0;
    }

    buffer = (uint8_t *)malloc(M2TS_READ_CHUNK + M2TS_PACKET_SIZE);
    if (!buffer) {
        fclose(file);
        return 0;
    }

    setvbuf(file, NULL, _IOFBF, M2TS_READ_CHUNK);
    while (!feof(file)) {
        size_t read_size = fread(buffer + carry, 1, M2TS_READ_CHUNK, file);
        size_t total = carry + read_size;
        size_t offset = 0;

        while (offset + M2TS_PACKET_SIZE <= total) {
            uint16_t pid = 0;
            size_t payload_size = parse_m2ts_packet_payload_size(buffer + offset, &pid);
            PG_STREAM_INFO *stream = probe_result_find_pg_stream(result, pid);
            if (stream && payload_size > 0) {
                stream->payload_bytes += (uint64_t)payload_size;
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
        success = 1;
    }

    free(buffer);
    fclose(file);
    return success;
}

static void print_probe_result_json(const char *disc_path, const PROBE_RESULT *result)
{
    size_t i;

    (void)disc_path;
    printf("{");
    printf("\"source\":");
    json_print_string(result && result->source[0] ? result->source : "unknown");
    printf(",\"clip\":{");
    printf("\"clip_id\":");
    json_print_string(result && result->clip_id[0] ? result->clip_id : "");
    printf(",\"pg_stream_count\":%zu", result ? result->pg_stream_count : 0u);
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
    int argi;
    int mpls_ok;
    int clpi_ok;

    if (argc < 4) {
        usage(argv[0]);
        return 2;
    }

    probe_result_init(&result);
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

    (void)probe_result_scan_clip_pg_bitrates(disc_path, &result);
    print_probe_result_json(disc_path, &result);
    probe_result_free(&result);
    return 0;
}
