// The scene's Metal producer: its own MTLDevice, rendering into IOSurfaces.
#include <stdint.h>

typedef struct m3d m3d;
typedef struct m3d_target m3d_target;

// m3d_new makes the device: the one whose registryID is registry when match is set, else the system
// default. Returns 0, or -1 with err set.
int m3d_new(uint64_t registry, int match, m3d **out, char *err, int errlen);
// m3d_scene uploads the scene: verts (32 bytes each: position, normal, uv as floats), the
// floor's and the crate's ranges of them, and the RGBA8 atlas. Returns 0, or -1 with err set.
int m3d_scene(m3d *m, const void *verts, uint32_t nverts, uint32_t floorFirst, uint32_t floorCount,
	uint32_t crateFirst, uint32_t crateCount, const uint8_t *atlas, int aw, int ah, char *err, int errlen);
const char *m3d_device_name(m3d *m);
// m3d_target_new makes a w×h BGRA IOSurface the device renders into. surface is its
// IOSurfaceRef, owned by the target: the window borrows it.
int m3d_target_new(m3d *m, int w, int h, m3d_target **out, uintptr_t *surface, char *err, int errlen);
// m3d_render draws the scene into t and waits for it: draws[0..31] is the floor's mvp and model,
// draws[32..63] the crate's.
int m3d_render(m3d *m, m3d_target *t, const float *draws, char *err, int errlen);
void m3d_target_free(m3d_target *t);
void m3d_free(m3d *m);
