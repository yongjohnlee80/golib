// The scene's Vulkan producer: its own VkDevice, rendering into images it exports as dma-bufs.
#include <stdint.h>

typedef struct s3d s3d;
typedef struct s3d_target s3d_target;

enum { S3D_RGBA8 = 0, S3D_BGRA8 = 1 };

// s3d_export is where a target's pixels are, for the window: one plane of a dma-buf.
typedef struct {
	int fd;          // owned by the target: the window borrows it
	uint32_t offset; // bytes
	uint32_t pitch;  // bytes per row
	uint64_t modifier;
} s3d_export;

// s3d_new makes the device: the physical device whose deviceUUID is uuid when match is set,
// else the first that exports dma-bufs. Returns 0, or -1 with err set.
int s3d_new(const uint8_t *uuid, int match, s3d **out, char *err, int errlen);
// s3d_scene uploads the scene: verts (32 bytes each: position, normal, uv as floats), the
// floor's and the crate's ranges of them, the RGBA8 atlas, and the SPIR-V shaders (copied, so
// they need no alignment). Returns 0, or -1 with err set.
int s3d_scene(s3d *s, const void *verts, uint32_t nverts, uint32_t floorFirst, uint32_t floorCount,
	uint32_t crateFirst, uint32_t crateCount, const uint8_t *atlas, int aw, int ah,
	const void *vs, uint32_t vsBytes, const void *fs, uint32_t fsBytes, char *err, int errlen);
// s3d_device_name is the physical device's name.
const char *s3d_device_name(s3d *s);
// s3d_has_modifiers reports VK_EXT_image_drm_format_modifier on the device.
int s3d_has_modifiers(s3d *s);
// s3d_modifiers lists up to max modifiers fmt can be rendered to and exported with, in one
// memory plane. Without the modifier extension it lists LINEAR (0) when linear tiling renders
// and exports. Returns the count.
int s3d_modifiers(s3d *s, int fmt, uint64_t *mods, int max);
// s3d_target_new makes a w×h image of fmt the device renders into and exports, with one of
// mods (the driver picks). Returns 0, or -1 with err set.
int s3d_target_new(s3d *s, int w, int h, int fmt, const uint64_t *mods, int nmods, s3d_target **out, s3d_export *ex, char *err, int errlen);
// s3d_readback_new makes a w×h target of fmt that is not exported, whose pixels s3d_read copies
// out: an OPTIMAL image in device-local memory where there is some, which each s3d_render copies
// into a host-visible buffer (cached where there is such memory) mapped for the target's life.
// It checks the rendering where no device exports, and on any GPU.
int s3d_readback_new(s3d *s, int w, int h, int fmt, s3d_target **out, char *err, int errlen);
// s3d_read copies a read-back target's last render into dst (w*4 bytes a row, tightly packed),
// invalidating the buffer first when its memory is not host-coherent. Call it after s3d_render,
// which has waited for the copy. Returns 0, or -1.
int s3d_read(s3d *s, s3d_target *t, uint8_t *dst, char *err, int errlen);
// s3d_render draws the scene into t and waits for it: draws[0..31] is the floor's mvp and model,
// draws[32..63] the crate's. An exported target's image is left released to the foreign queue
// family, in the GENERAL layout, for the window to acquire. A read-back target's image stays on
// this queue, in TRANSFER_SRC_OPTIMAL, its pixels copied into its host buffer and visible to the
// host. Returns 0, or -1 with err set.
int s3d_render(s3d *s, s3d_target *t, const float *draws, char *err, int errlen);
void s3d_target_free(s3d *s, s3d_target *t);
void s3d_free(s3d *s);
