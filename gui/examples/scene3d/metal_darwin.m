// The scene's Metal producer (metal_darwin.h). Built with ARC.
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#import <CoreFoundation/CoreFoundation.h>
#import <IOSurface/IOSurfaceRef.h>
#import <Metal/Metal.h>

#include "metal_darwin.h"

// The scene's shaders: shaders/scene.vert and scene.frag, in MSL.
static NSString *const source = @"#include <metal_stdlib>\n"
	"using namespace metal;\n"
	"struct Vert { packed_float3 pos; packed_float3 normal; packed_float2 uv; };\n"
	"struct Draw { float4x4 mvp; float4x4 model; };\n"
	"struct Out { float4 pos [[position]]; float2 uv; float light; };\n"
	"vertex Out vmain(uint vid [[vertex_id]], const device Vert *verts [[buffer(0)]], constant Draw &draw [[buffer(1)]]) {\n"
	"	Vert v = verts[vid];\n"
	"	Out o;\n"
	"	o.pos = draw.mvp * float4(float3(v.pos), 1.0);\n"
	"	float3 n = (draw.model * float4(float3(v.normal), 0.0)).xyz;\n"
	"	o.light = 0.35 + 0.65 * max(0.0, dot(n, normalize(float3(0.4, 0.8, 0.45))));\n"
	"	o.uv = float2(v.uv);\n"
	"	return o;\n"
	"}\n"
	"fragment float4 fmain(Out in [[stage_in]], texture2d<float> atlas [[texture(0)]], sampler s [[sampler(0)]]) {\n"
	"	return float4(atlas.sample(s, in.uv).rgb * in.light, 1.0);\n"
	"}\n";

struct m3d {
	id<MTLDevice> dev;
	id<MTLCommandQueue> queue;
	id<MTLRenderPipelineState> pipe;
	id<MTLDepthStencilState> depth;
	id<MTLBuffer> verts;
	id<MTLTexture> atlas;
	id<MTLSamplerState> sampler;
	uint32_t floorFirst, floorCount, crateFirst, crateCount;
	char name[256];
};

struct m3d_target {
	int w, h;
	IOSurfaceRef surface;
	id<MTLTexture> colour;
	id<MTLTexture> depth;
};

static int fail(char *err, int errlen, const char *fmt, ...) {
	va_list ap;
	va_start(ap, fmt);
	vsnprintf(err, (size_t)errlen, fmt, ap);
	va_end(ap);
	return -1;
}

int m3d_new(uint64_t registry, int match, m3d **out, char *err, int errlen) {
	m3d *m = calloc(1, sizeof *m);
	*out = m;
	@autoreleasepool {
		if (match) {
			for (id<MTLDevice> d in MTLCopyAllDevices()) {
				if (d.registryID == registry) m->dev = d;
			}
		} else {
			m->dev = MTLCreateSystemDefaultDevice();
		}
		if (m->dev == nil) return fail(err, errlen, match ? "no Metal device with the window's registryID" : "no Metal device");
		snprintf(m->name, sizeof m->name, "%s", m->dev.name.UTF8String);
		m->queue = [m->dev newCommandQueue];
		NSError *e = nil;
		id<MTLLibrary> lib = [m->dev newLibraryWithSource:source options:nil error:&e];
		if (lib == nil) return fail(err, errlen, "MSL: %s", e.localizedDescription.UTF8String);
		MTLRenderPipelineDescriptor *pd = [MTLRenderPipelineDescriptor new];
		pd.vertexFunction = [lib newFunctionWithName:@"vmain"];
		pd.fragmentFunction = [lib newFunctionWithName:@"fmain"];
		pd.colorAttachments[0].pixelFormat = MTLPixelFormatBGRA8Unorm;
		pd.depthAttachmentPixelFormat = MTLPixelFormatDepth32Float;
		m->pipe = [m->dev newRenderPipelineStateWithDescriptor:pd error:&e];
		if (m->pipe == nil) return fail(err, errlen, "pipeline: %s", e.localizedDescription.UTF8String);
		MTLDepthStencilDescriptor *dd = [MTLDepthStencilDescriptor new];
		dd.depthCompareFunction = MTLCompareFunctionLess;
		dd.depthWriteEnabled = YES;
		m->depth = [m->dev newDepthStencilStateWithDescriptor:dd];
		MTLSamplerDescriptor *sd = [MTLSamplerDescriptor new];
		sd.minFilter = MTLSamplerMinMagFilterNearest;
		sd.magFilter = MTLSamplerMinMagFilterNearest;
		sd.sAddressMode = MTLSamplerAddressModeClampToEdge;
		sd.tAddressMode = MTLSamplerAddressModeClampToEdge;
		m->sampler = [m->dev newSamplerStateWithDescriptor:sd];
	}
	return 0;
}

const char *m3d_device_name(m3d *m) { return m->name; }

int m3d_scene(m3d *m, const void *verts, uint32_t nverts, uint32_t floorFirst, uint32_t floorCount,
	uint32_t crateFirst, uint32_t crateCount, const uint8_t *atlas, int aw, int ah, char *err, int errlen) {
	m->floorFirst = floorFirst, m->floorCount = floorCount;
	m->crateFirst = crateFirst, m->crateCount = crateCount;
	m->verts = [m->dev newBufferWithBytes:verts length:(NSUInteger)nverts * 32 options:MTLResourceStorageModeShared];
	MTLTextureDescriptor *td = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm
		width:(NSUInteger)aw height:(NSUInteger)ah mipmapped:NO];
	td.usage = MTLTextureUsageShaderRead;
	m->atlas = [m->dev newTextureWithDescriptor:td];
	if (m->verts == nil || m->atlas == nil) return fail(err, errlen, "no memory for the scene");
	[m->atlas replaceRegion:MTLRegionMake2D(0, 0, (NSUInteger)aw, (NSUInteger)ah) mipmapLevel:0 withBytes:atlas bytesPerRow:(NSUInteger)aw * 4];
	return 0;
}

static void setInt(CFMutableDictionaryRef d, CFStringRef k, int v) {
	CFNumberRef n = CFNumberCreate(NULL, kCFNumberIntType, &v);
	CFDictionarySetValue(d, k, n);
	CFRelease(n);
}

int m3d_target_new(m3d *m, int w, int h, m3d_target **out, uintptr_t *surface, char *err, int errlen) {
	m3d_target *t = calloc(1, sizeof *t);
	*out = t;
	t->w = w, t->h = h;
	CFMutableDictionaryRef p = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	setInt(p, kIOSurfaceWidth, w);
	setInt(p, kIOSurfaceHeight, h);
	setInt(p, kIOSurfaceBytesPerElement, 4);
	setInt(p, kIOSurfacePixelFormat, 'BGRA');
	t->surface = IOSurfaceCreate(p);
	CFRelease(p);
	if (t->surface == NULL) return fail(err, errlen, "IOSurfaceCreate %dx%d", w, h);
	MTLTextureDescriptor *td = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatBGRA8Unorm
		width:(NSUInteger)w height:(NSUInteger)h mipmapped:NO];
	td.usage = MTLTextureUsageRenderTarget;
	td.storageMode = MTLStorageModeManaged; // a texture over an IOSurface on macOS
	t->colour = [m->dev newTextureWithDescriptor:td iosurface:t->surface plane:0];
	MTLTextureDescriptor *dd = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatDepth32Float
		width:(NSUInteger)w height:(NSUInteger)h mipmapped:NO];
	dd.usage = MTLTextureUsageRenderTarget;
	dd.storageMode = MTLStorageModePrivate;
	t->depth = [m->dev newTextureWithDescriptor:dd];
	if (t->colour == nil || t->depth == nil) return fail(err, errlen, "no render target over the IOSurface");
	*surface = (uintptr_t)t->surface;
	return 0;
}

int m3d_render(m3d *m, m3d_target *t, const float *draws, char *err, int errlen) {
	@autoreleasepool {
		MTLRenderPassDescriptor *rp = [MTLRenderPassDescriptor renderPassDescriptor];
		rp.colorAttachments[0].texture = t->colour;
		rp.colorAttachments[0].loadAction = MTLLoadActionClear;
		rp.colorAttachments[0].storeAction = MTLStoreActionStore;
		rp.colorAttachments[0].clearColor = MTLClearColorMake(0x1a / 255.0, 0x1d / 255.0, 0x2b / 255.0, 1); // clearColour in scene.go
		rp.depthAttachment.texture = t->depth;
		rp.depthAttachment.loadAction = MTLLoadActionClear;
		rp.depthAttachment.storeAction = MTLStoreActionDontCare;
		rp.depthAttachment.clearDepth = 1;
		id<MTLCommandBuffer> cb = [m->queue commandBuffer];
		id<MTLRenderCommandEncoder> enc = [cb renderCommandEncoderWithDescriptor:rp];
		[enc setRenderPipelineState:m->pipe];
		[enc setDepthStencilState:m->depth];
		[enc setVertexBuffer:m->verts offset:0 atIndex:0];
		[enc setFragmentTexture:m->atlas atIndex:0];
		[enc setFragmentSamplerState:m->sampler atIndex:0];
		// Metal's framebuffer y points down and its clip y up, as the scene's projection takes it.
		[enc setVertexBytes:draws length:128 atIndex:1];
		[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:m->floorFirst vertexCount:m->floorCount];
		[enc setVertexBytes:draws + 32 length:128 atIndex:1];
		[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:m->crateFirst vertexCount:m->crateCount];
		[enc endEncoding];
		[cb commit];
		[cb waitUntilCompleted]; // a frame is complete when it is made
		if (cb.status != MTLCommandBufferStatusCompleted) {
			return fail(err, errlen, "command buffer: %s", cb.error.localizedDescription.UTF8String);
		}
	}
	return 0;
}

void m3d_target_free(m3d_target *t) {
	if (!t) return;
	t->colour = nil;
	t->depth = nil;
	if (t->surface) CFRelease(t->surface);
	free(t);
}

void m3d_free(m3d *m) {
	if (!m) return;
	m->dev = nil;
	m->queue = nil;
	m->pipe = nil;
	m->depth = nil;
	m->verts = nil;
	m->atlas = nil;
	m->sampler = nil;
	free(m);
}
