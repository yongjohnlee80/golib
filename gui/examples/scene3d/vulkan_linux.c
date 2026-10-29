// The scene's Vulkan producer (vulkan_linux.h). Plain Vulkan 1.1 with
// VK_KHR_external_memory_fd and VK_EXT_external_memory_dma_buf; VK_EXT_image_drm_format_modifier
// and VK_EXT_queue_family_foreign when the device has them.
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <vulkan/vulkan.h>

#include "vulkan_linux.h"

#define MAX_MODS 64

struct s3d {
	VkInstance inst;
	VkPhysicalDevice pd;
	VkPhysicalDeviceProperties props;
	VkPhysicalDeviceMemoryProperties mem;
	VkDevice dev;
	VkQueue queue;
	uint32_t family;
	int modifiers; // VK_EXT_image_drm_format_modifier
	int foreign;   // VK_EXT_queue_family_foreign
	PFN_vkGetMemoryFdKHR getMemoryFd;
	PFN_vkGetImageDrmFormatModifierPropertiesEXT getModifierProps;

	VkCommandPool pool;
	VkCommandBuffer cmd;
	VkFence fence;

	// The scene, from s3d_scene.
	VkBuffer vbuf;
	VkDeviceMemory vmem;
	uint32_t floorFirst, floorCount, crateFirst, crateCount;
	VkImage atlas;
	VkDeviceMemory atlasMem;
	VkImageView atlasView;
	VkSampler sampler;
	VkDescriptorSetLayout setLayout;
	VkDescriptorPool descPool;
	VkDescriptorSet set;
	VkPipelineLayout layout;
	VkShaderModule vs, fs;

	// The pipeline, for the targets' colour format (made with the first target).
	VkFormat colour;
	VkRenderPass pass;
	VkPipeline pipe;
};

struct s3d_target {
	int w, h;
	VkImage img;
	VkDeviceMemory mem;
	VkImageView view;
	VkImage depth;
	VkDeviceMemory depthMem;
	VkImageView depthView;
	VkFramebuffer fb;
	int fd;
	int exported; // released to the foreign queue family after each render
	VkDeviceSize offset, pitch; // a read-back target's layout
	// A read-back target renders into a local OPTIMAL image (localImage) and copies it into this
	// host-visible buffer, w*h*4 bytes tightly packed: rendering straight into a LINEAR image in
	// host memory is not something every GPU does (NVIDIA loses the device).
	VkBuffer readBuf;
	VkDeviceMemory readMem;
	void *readMap;     // readMem, mapped for the target's life
	int readCoherent;  // else invalidated before each read
};

static int fail(char *err, int errlen, const char *fmt, ...) {
	va_list ap;
	va_start(ap, fmt);
	vsnprintf(err, (size_t)errlen, fmt, ap);
	va_end(ap);
	return -1;
}

#define CHECK(call, what) \
	do { \
		VkResult r_ = (call); \
		if (r_ != VK_SUCCESS) return fail(err, errlen, "%s: VkResult %d", what, (int)r_); \
	} while (0)

static VkFormat vkFormat(int fmt) {
	return fmt == S3D_BGRA8 ? VK_FORMAT_B8G8R8A8_UNORM : VK_FORMAT_R8G8B8A8_UNORM;
}

static int hasExt(VkExtensionProperties *ext, uint32_t n, const char *name) {
	for (uint32_t i = 0; i < n; i++) {
		if (strcmp(ext[i].extensionName, name) == 0) return 1;
	}
	return 0;
}

// memType is a memory type in bits with props, preferring one with all of want; -1 if none.
static int memType(s3d *s, uint32_t bits, VkMemoryPropertyFlags want) {
	for (uint32_t i = 0; i < s->mem.memoryTypeCount; i++) {
		if ((bits & (1u << i)) && (s->mem.memoryTypes[i].propertyFlags & want) == want) return (int)i;
	}
	for (uint32_t i = 0; i < s->mem.memoryTypeCount; i++) {
		if (bits & (1u << i)) return (int)i;
	}
	return -1;
}

int s3d_new(const uint8_t *uuid, int match, s3d **out, char *err, int errlen) {
	s3d *s = calloc(1, sizeof *s);
	*out = s;
	VkApplicationInfo app = {
		.sType = VK_STRUCTURE_TYPE_APPLICATION_INFO,
		.pApplicationName = "scene3d",
		.apiVersion = VK_API_VERSION_1_1,
	};
	VkInstanceCreateInfo ici = {.sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO, .pApplicationInfo = &app};
	CHECK(vkCreateInstance(&ici, NULL, &s->inst), "vkCreateInstance");

	uint32_t n = 0;
	vkEnumeratePhysicalDevices(s->inst, &n, NULL);
	VkPhysicalDevice pds[16];
	n = n > 16 ? 16 : n;
	vkEnumeratePhysicalDevices(s->inst, &n, pds);
	for (uint32_t i = 0; i < n && !s->pd; i++) {
		VkPhysicalDeviceIDProperties id = {.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_ID_PROPERTIES};
		VkPhysicalDeviceProperties2 p2 = {.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2, .pNext = &id};
		vkGetPhysicalDeviceProperties2(pds[i], &p2);
		if (p2.properties.apiVersion < VK_API_VERSION_1_1) continue;
		if (match && memcmp(id.deviceUUID, uuid, VK_UUID_SIZE) != 0) continue;
		uint32_t ne = 0;
		vkEnumerateDeviceExtensionProperties(pds[i], NULL, &ne, NULL);
		VkExtensionProperties *ext = calloc(ne, sizeof *ext);
		vkEnumerateDeviceExtensionProperties(pds[i], NULL, &ne, ext);
		int ok = hasExt(ext, ne, VK_KHR_EXTERNAL_MEMORY_FD_EXTENSION_NAME) &&
			hasExt(ext, ne, VK_EXT_EXTERNAL_MEMORY_DMA_BUF_EXTENSION_NAME);
		if (ok) {
			s->pd = pds[i];
			s->props = p2.properties;
			s->modifiers = hasExt(ext, ne, VK_EXT_IMAGE_DRM_FORMAT_MODIFIER_EXTENSION_NAME) &&
				hasExt(ext, ne, VK_KHR_IMAGE_FORMAT_LIST_EXTENSION_NAME);
			s->foreign = hasExt(ext, ne, VK_EXT_QUEUE_FAMILY_FOREIGN_EXTENSION_NAME);
		}
		free(ext);
	}
	if (!s->pd) {
		return fail(err, errlen, match ? "no Vulkan device with the window's UUID exports dma-bufs" : "no Vulkan device exports dma-bufs");
	}
	vkGetPhysicalDeviceMemoryProperties(s->pd, &s->mem);

	uint32_t nf = 0;
	vkGetPhysicalDeviceQueueFamilyProperties(s->pd, &nf, NULL);
	VkQueueFamilyProperties fams[16];
	nf = nf > 16 ? 16 : nf;
	vkGetPhysicalDeviceQueueFamilyProperties(s->pd, &nf, fams);
	s->family = UINT32_MAX;
	for (uint32_t i = 0; i < nf; i++) {
		if (fams[i].queueFlags & VK_QUEUE_GRAPHICS_BIT) {
			s->family = i;
			break;
		}
	}
	if (s->family == UINT32_MAX) return fail(err, errlen, "no graphics queue");

	const char *exts[6];
	uint32_t ne = 0;
	exts[ne++] = VK_KHR_EXTERNAL_MEMORY_FD_EXTENSION_NAME;
	exts[ne++] = VK_EXT_EXTERNAL_MEMORY_DMA_BUF_EXTENSION_NAME;
	if (s->modifiers) {
		exts[ne++] = VK_EXT_IMAGE_DRM_FORMAT_MODIFIER_EXTENSION_NAME;
		exts[ne++] = VK_KHR_IMAGE_FORMAT_LIST_EXTENSION_NAME;
	}
	if (s->foreign) exts[ne++] = VK_EXT_QUEUE_FAMILY_FOREIGN_EXTENSION_NAME;
	float prio = 1;
	VkDeviceQueueCreateInfo qci = {
		.sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
		.queueFamilyIndex = s->family,
		.queueCount = 1,
		.pQueuePriorities = &prio,
	};
	VkDeviceCreateInfo dci = {
		.sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
		.queueCreateInfoCount = 1,
		.pQueueCreateInfos = &qci,
		.enabledExtensionCount = ne,
		.ppEnabledExtensionNames = exts,
	};
	CHECK(vkCreateDevice(s->pd, &dci, NULL, &s->dev), "vkCreateDevice");
	vkGetDeviceQueue(s->dev, s->family, 0, &s->queue);
	s->getMemoryFd = (PFN_vkGetMemoryFdKHR)vkGetDeviceProcAddr(s->dev, "vkGetMemoryFdKHR");
	if (s->modifiers) {
		s->getModifierProps = (PFN_vkGetImageDrmFormatModifierPropertiesEXT)vkGetDeviceProcAddr(s->dev, "vkGetImageDrmFormatModifierPropertiesEXT");
	}
	if (!s->getMemoryFd || (s->modifiers && !s->getModifierProps)) return fail(err, errlen, "missing extension entry points");

	VkCommandPoolCreateInfo cpi = {
		.sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
		.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT,
		.queueFamilyIndex = s->family,
	};
	CHECK(vkCreateCommandPool(s->dev, &cpi, NULL, &s->pool), "vkCreateCommandPool");
	VkCommandBufferAllocateInfo cai = {
		.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
		.commandPool = s->pool,
		.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY,
		.commandBufferCount = 1,
	};
	CHECK(vkAllocateCommandBuffers(s->dev, &cai, &s->cmd), "vkAllocateCommandBuffers");
	VkFenceCreateInfo fci = {.sType = VK_STRUCTURE_TYPE_FENCE_CREATE_INFO};
	CHECK(vkCreateFence(s->dev, &fci, NULL, &s->fence), "vkCreateFence");
	return 0;
}

const char *s3d_device_name(s3d *s) { return s->props.deviceName; }

int s3d_has_modifiers(s3d *s) { return s->modifiers; }

// exportable reports that fmt with tiling (and mod, for modifier tiling) renders as a colour
// attachment and exports as a dma-buf.
static int exportable(s3d *s, VkFormat fmt, VkImageTiling tiling, uint64_t mod) {
	VkPhysicalDeviceImageDrmFormatModifierInfoEXT modInfo = {
		.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_IMAGE_DRM_FORMAT_MODIFIER_INFO_EXT,
		.drmFormatModifier = mod,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE,
	};
	VkPhysicalDeviceExternalImageFormatInfo extInfo = {
		.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_EXTERNAL_IMAGE_FORMAT_INFO,
		.pNext = tiling == VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT ? &modInfo : NULL,
		.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT,
	};
	VkPhysicalDeviceImageFormatInfo2 info = {
		.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_IMAGE_FORMAT_INFO_2,
		.pNext = &extInfo,
		.format = fmt,
		.type = VK_IMAGE_TYPE_2D,
		.tiling = tiling,
		.usage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT,
	};
	VkExternalImageFormatProperties extProps = {.sType = VK_STRUCTURE_TYPE_EXTERNAL_IMAGE_FORMAT_PROPERTIES};
	VkImageFormatProperties2 props = {.sType = VK_STRUCTURE_TYPE_IMAGE_FORMAT_PROPERTIES_2, .pNext = &extProps};
	if (vkGetPhysicalDeviceImageFormatProperties2(s->pd, &info, &props) != VK_SUCCESS) return 0;
	return (extProps.externalMemoryProperties.externalMemoryFeatures & VK_EXTERNAL_MEMORY_FEATURE_EXPORTABLE_BIT) != 0;
}

int s3d_modifiers(s3d *s, int fmt, uint64_t *mods, int max) {
	VkFormat f = vkFormat(fmt);
	int n = 0;
	if (!s->modifiers) {
		VkFormatProperties fp;
		vkGetPhysicalDeviceFormatProperties(s->pd, f, &fp);
		if ((fp.linearTilingFeatures & VK_FORMAT_FEATURE_COLOR_ATTACHMENT_BIT) && max > 0 &&
			exportable(s, f, VK_IMAGE_TILING_LINEAR, 0)) {
			mods[n++] = 0; // DRM_FORMAT_MOD_LINEAR
		}
		return n;
	}
	VkDrmFormatModifierPropertiesEXT props[MAX_MODS];
	VkDrmFormatModifierPropertiesListEXT list = {
		.sType = VK_STRUCTURE_TYPE_DRM_FORMAT_MODIFIER_PROPERTIES_LIST_EXT,
		.drmFormatModifierCount = MAX_MODS,
		.pDrmFormatModifierProperties = props,
	};
	VkFormatProperties2 fp2 = {.sType = VK_STRUCTURE_TYPE_FORMAT_PROPERTIES_2, .pNext = &list};
	vkGetPhysicalDeviceFormatProperties2(s->pd, f, &fp2);
	for (uint32_t i = 0; i < list.drmFormatModifierCount && n < max; i++) {
		VkDrmFormatModifierPropertiesEXT p = props[i];
		if (p.drmFormatModifierPlaneCount != 1) continue;
		if (!(p.drmFormatModifierTilingFeatures & VK_FORMAT_FEATURE_COLOR_ATTACHMENT_BIT)) continue;
		if (!exportable(s, f, VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT, p.drmFormatModifier)) continue;
		mods[n++] = p.drmFormatModifier;
	}
	return n;
}

// oneShot records with rec, submits and waits.
static int oneShot(s3d *s, void (*rec)(s3d *, VkCommandBuffer, void *), void *arg, char *err, int errlen) {
	VkCommandBufferBeginInfo bi = {
		.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,
		.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,
	};
	CHECK(vkResetCommandBuffer(s->cmd, 0), "vkResetCommandBuffer");
	CHECK(vkBeginCommandBuffer(s->cmd, &bi), "vkBeginCommandBuffer");
	rec(s, s->cmd, arg);
	CHECK(vkEndCommandBuffer(s->cmd), "vkEndCommandBuffer");
	VkSubmitInfo si = {.sType = VK_STRUCTURE_TYPE_SUBMIT_INFO, .commandBufferCount = 1, .pCommandBuffers = &s->cmd};
	CHECK(vkResetFences(s->dev, 1, &s->fence), "vkResetFences");
	CHECK(vkQueueSubmit(s->queue, 1, &si, s->fence), "vkQueueSubmit");
	CHECK(vkWaitForFences(s->dev, 1, &s->fence, VK_TRUE, UINT64_MAX), "vkWaitForFences");
	return 0;
}

static int hostBuffer(s3d *s, VkDeviceSize size, VkBufferUsageFlags usage, const void *data, VkBuffer *buf, VkDeviceMemory *mem, char *err, int errlen) {
	VkBufferCreateInfo bci = {
		.sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
		.size = size,
		.usage = usage,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE,
	};
	CHECK(vkCreateBuffer(s->dev, &bci, NULL, buf), "vkCreateBuffer");
	VkMemoryRequirements req;
	vkGetBufferMemoryRequirements(s->dev, *buf, &req);
	int t = memType(s, req.memoryTypeBits, VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
	if (t < 0 || !(s->mem.memoryTypes[t].propertyFlags & VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT)) {
		return fail(err, errlen, "no host-visible memory for a buffer");
	}
	VkMemoryAllocateInfo mai = {.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, .allocationSize = req.size, .memoryTypeIndex = (uint32_t)t};
	CHECK(vkAllocateMemory(s->dev, &mai, NULL, mem), "vkAllocateMemory");
	CHECK(vkBindBufferMemory(s->dev, *buf, *mem, 0), "vkBindBufferMemory");
	void *p;
	CHECK(vkMapMemory(s->dev, *mem, 0, size, 0, &p), "vkMapMemory");
	memcpy(p, data, size);
	if (!(s->mem.memoryTypes[t].propertyFlags & VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)) {
		VkMappedMemoryRange r = {.sType = VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE, .memory = *mem, .size = VK_WHOLE_SIZE};
		vkFlushMappedMemoryRanges(s->dev, 1, &r);
	}
	vkUnmapMemory(s->dev, *mem);
	return 0;
}

// localImage makes a device-local optimal image and its view.
static int localImage(s3d *s, int w, int h, VkFormat fmt, VkImageUsageFlags usage, VkImageAspectFlags aspect,
	VkImage *img, VkDeviceMemory *mem, VkImageView *view, char *err, int errlen) {
	VkImageCreateInfo ici = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
		.imageType = VK_IMAGE_TYPE_2D,
		.format = fmt,
		.extent = {(uint32_t)w, (uint32_t)h, 1},
		.mipLevels = 1,
		.arrayLayers = 1,
		.samples = VK_SAMPLE_COUNT_1_BIT,
		.tiling = VK_IMAGE_TILING_OPTIMAL,
		.usage = usage,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE,
		.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
	};
	CHECK(vkCreateImage(s->dev, &ici, NULL, img), "vkCreateImage");
	VkMemoryRequirements req;
	vkGetImageMemoryRequirements(s->dev, *img, &req);
	int t = memType(s, req.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
	if (t < 0) return fail(err, errlen, "no memory for an image");
	VkMemoryAllocateInfo mai = {.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, .allocationSize = req.size, .memoryTypeIndex = (uint32_t)t};
	CHECK(vkAllocateMemory(s->dev, &mai, NULL, mem), "vkAllocateMemory");
	CHECK(vkBindImageMemory(s->dev, *img, *mem, 0), "vkBindImageMemory");
	VkImageViewCreateInfo vci = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
		.image = *img,
		.viewType = VK_IMAGE_VIEW_TYPE_2D,
		.format = fmt,
		.subresourceRange = {aspect, 0, 1, 0, 1},
	};
	CHECK(vkCreateImageView(s->dev, &vci, NULL, view), "vkCreateImageView");
	return 0;
}

struct upload {
	VkBuffer staging;
	int w, h;
};

static void recUpload(s3d *s, VkCommandBuffer cb, void *arg) {
	struct upload *u = arg;
	VkImageMemoryBarrier toDst = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,
		.dstAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT,
		.oldLayout = VK_IMAGE_LAYOUT_UNDEFINED,
		.newLayout = VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.image = s->atlas,
		.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
	};
	vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, 0, 0, NULL, 0, NULL, 1, &toDst);
	VkBufferImageCopy copy = {
		.imageSubresource = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 0, 1},
		.imageExtent = {(uint32_t)u->w, (uint32_t)u->h, 1},
	};
	vkCmdCopyBufferToImage(cb, u->staging, s->atlas, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &copy);
	VkImageMemoryBarrier toRead = toDst;
	toRead.srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT;
	toRead.dstAccessMask = VK_ACCESS_SHADER_READ_BIT;
	toRead.oldLayout = VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL;
	toRead.newLayout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
	vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT, 0, 0, NULL, 0, NULL, 1, &toRead);
}

static int shader(s3d *s, const void *code, uint32_t n, VkShaderModule *out, char *err, int errlen) {
	uint32_t *aligned = malloc(n);
	memcpy(aligned, code, n);
	VkShaderModuleCreateInfo sci = {.sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO, .codeSize = n, .pCode = aligned};
	VkResult r = vkCreateShaderModule(s->dev, &sci, NULL, out);
	free(aligned);
	CHECK(r, "vkCreateShaderModule");
	return 0;
}

int s3d_scene(s3d *s, const void *verts, uint32_t nverts, uint32_t floorFirst, uint32_t floorCount,
	uint32_t crateFirst, uint32_t crateCount, const uint8_t *atlas, int aw, int ah,
	const void *vs, uint32_t vsBytes, const void *fs, uint32_t fsBytes, char *err, int errlen) {
	s->floorFirst = floorFirst, s->floorCount = floorCount;
	s->crateFirst = crateFirst, s->crateCount = crateCount;
	if (hostBuffer(s, (VkDeviceSize)nverts * 32, VK_BUFFER_USAGE_VERTEX_BUFFER_BIT, verts, &s->vbuf, &s->vmem, err, errlen)) return -1;

	if (localImage(s, aw, ah, VK_FORMAT_R8G8B8A8_UNORM, VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT,
		VK_IMAGE_ASPECT_COLOR_BIT, &s->atlas, &s->atlasMem, &s->atlasView, err, errlen)) return -1;
	struct upload u = {.w = aw, .h = ah};
	VkDeviceMemory stagingMem;
	if (hostBuffer(s, (VkDeviceSize)aw * ah * 4, VK_BUFFER_USAGE_TRANSFER_SRC_BIT, atlas, &u.staging, &stagingMem, err, errlen)) return -1;
	int r = oneShot(s, recUpload, &u, err, errlen);
	vkDestroyBuffer(s->dev, u.staging, NULL);
	vkFreeMemory(s->dev, stagingMem, NULL);
	if (r) return -1;

	VkSamplerCreateInfo sci = {
		.sType = VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,
		.magFilter = VK_FILTER_NEAREST,
		.minFilter = VK_FILTER_NEAREST,
		.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
	};
	CHECK(vkCreateSampler(s->dev, &sci, NULL, &s->sampler), "vkCreateSampler");
	VkDescriptorSetLayoutBinding b = {
		.binding = 0,
		.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
		.descriptorCount = 1,
		.stageFlags = VK_SHADER_STAGE_FRAGMENT_BIT,
	};
	VkDescriptorSetLayoutCreateInfo dli = {.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, .bindingCount = 1, .pBindings = &b};
	CHECK(vkCreateDescriptorSetLayout(s->dev, &dli, NULL, &s->setLayout), "vkCreateDescriptorSetLayout");
	VkDescriptorPoolSize ps = {VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 1};
	VkDescriptorPoolCreateInfo dpi = {.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, .maxSets = 1, .poolSizeCount = 1, .pPoolSizes = &ps};
	CHECK(vkCreateDescriptorPool(s->dev, &dpi, NULL, &s->descPool), "vkCreateDescriptorPool");
	VkDescriptorSetAllocateInfo dai = {
		.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,
		.descriptorPool = s->descPool,
		.descriptorSetCount = 1,
		.pSetLayouts = &s->setLayout,
	};
	CHECK(vkAllocateDescriptorSets(s->dev, &dai, &s->set), "vkAllocateDescriptorSets");
	VkDescriptorImageInfo dii = {.sampler = s->sampler, .imageView = s->atlasView, .imageLayout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
	VkWriteDescriptorSet w = {
		.sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
		.dstSet = s->set,
		.descriptorCount = 1,
		.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
		.pImageInfo = &dii,
	};
	vkUpdateDescriptorSets(s->dev, 1, &w, 0, NULL);

	VkPushConstantRange pcr = {VK_SHADER_STAGE_VERTEX_BIT, 0, 128}; // mvp and model
	VkPipelineLayoutCreateInfo pli = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
		.setLayoutCount = 1,
		.pSetLayouts = &s->setLayout,
		.pushConstantRangeCount = 1,
		.pPushConstantRanges = &pcr,
	};
	CHECK(vkCreatePipelineLayout(s->dev, &pli, NULL, &s->layout), "vkCreatePipelineLayout");
	if (shader(s, vs, vsBytes, &s->vs, err, errlen) || shader(s, fs, fsBytes, &s->fs, err, errlen)) return -1;
	return 0;
}

// pipeline makes the render pass and pipeline for colour targets of fmt.
static int pipeline(s3d *s, VkFormat fmt, char *err, int errlen) {
	VkAttachmentDescription att[2] = {
		{
			.format = fmt,
			.samples = VK_SAMPLE_COUNT_1_BIT,
			.loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR,
			.storeOp = VK_ATTACHMENT_STORE_OP_STORE,
			.stencilLoadOp = VK_ATTACHMENT_LOAD_OP_DONT_CARE,
			.stencilStoreOp = VK_ATTACHMENT_STORE_OP_DONT_CARE,
			.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED, // every pixel is drawn again
			.finalLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
		},
		{
			.format = VK_FORMAT_D32_SFLOAT,
			.samples = VK_SAMPLE_COUNT_1_BIT,
			.loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR,
			.storeOp = VK_ATTACHMENT_STORE_OP_DONT_CARE,
			.stencilLoadOp = VK_ATTACHMENT_LOAD_OP_DONT_CARE,
			.stencilStoreOp = VK_ATTACHMENT_STORE_OP_DONT_CARE,
			.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
			.finalLayout = VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL,
		},
	};
	VkAttachmentReference colourRef = {0, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL};
	VkAttachmentReference depthRef = {1, VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL};
	VkSubpassDescription sub = {
		.pipelineBindPoint = VK_PIPELINE_BIND_POINT_GRAPHICS,
		.colorAttachmentCount = 1,
		.pColorAttachments = &colourRef,
		.pDepthStencilAttachment = &depthRef,
	};
	VkRenderPassCreateInfo rpi = {
		.sType = VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,
		.attachmentCount = 2,
		.pAttachments = att,
		.subpassCount = 1,
		.pSubpasses = &sub,
	};
	CHECK(vkCreateRenderPass(s->dev, &rpi, NULL, &s->pass), "vkCreateRenderPass");

	VkPipelineShaderStageCreateInfo stages[2] = {
		{.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, .stage = VK_SHADER_STAGE_VERTEX_BIT, .module = s->vs, .pName = "main"},
		{.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, .stage = VK_SHADER_STAGE_FRAGMENT_BIT, .module = s->fs, .pName = "main"},
	};
	VkVertexInputBindingDescription vb = {0, 32, VK_VERTEX_INPUT_RATE_VERTEX};
	VkVertexInputAttributeDescription va[3] = {
		{0, 0, VK_FORMAT_R32G32B32_SFLOAT, 0},
		{1, 0, VK_FORMAT_R32G32B32_SFLOAT, 12},
		{2, 0, VK_FORMAT_R32G32_SFLOAT, 24},
	};
	VkPipelineVertexInputStateCreateInfo vi = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
		.vertexBindingDescriptionCount = 1,
		.pVertexBindingDescriptions = &vb,
		.vertexAttributeDescriptionCount = 3,
		.pVertexAttributeDescriptions = va,
	};
	VkPipelineInputAssemblyStateCreateInfo ia = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,
		.topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST,
	};
	VkPipelineViewportStateCreateInfo vp = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,
		.viewportCount = 1,
		.scissorCount = 1,
	};
	VkPipelineRasterizationStateCreateInfo rs = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
		.polygonMode = VK_POLYGON_MODE_FILL,
		.cullMode = VK_CULL_MODE_NONE, // the depth test hides the far faces, as on the CPU
		.lineWidth = 1,
	};
	VkPipelineMultisampleStateCreateInfo ms = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
		.rasterizationSamples = VK_SAMPLE_COUNT_1_BIT,
	};
	VkPipelineDepthStencilStateCreateInfo ds = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,
		.depthTestEnable = VK_TRUE,
		.depthWriteEnable = VK_TRUE,
		.depthCompareOp = VK_COMPARE_OP_LESS,
	};
	VkPipelineColorBlendAttachmentState cba = {.colorWriteMask = 0xf};
	VkPipelineColorBlendStateCreateInfo cb = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
		.attachmentCount = 1,
		.pAttachments = &cba,
	};
	VkDynamicState dyn[2] = {VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR};
	VkPipelineDynamicStateCreateInfo dy = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,
		.dynamicStateCount = 2,
		.pDynamicStates = dyn,
	};
	VkGraphicsPipelineCreateInfo gpi = {
		.sType = VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,
		.stageCount = 2,
		.pStages = stages,
		.pVertexInputState = &vi,
		.pInputAssemblyState = &ia,
		.pViewportState = &vp,
		.pRasterizationState = &rs,
		.pMultisampleState = &ms,
		.pDepthStencilState = &ds,
		.pColorBlendState = &cb,
		.pDynamicState = &dy,
		.layout = s->layout,
		.renderPass = s->pass,
	};
	CHECK(vkCreateGraphicsPipelines(s->dev, VK_NULL_HANDLE, 1, &gpi, NULL, &s->pipe), "vkCreateGraphicsPipelines");
	s->colour = fmt;
	return 0;
}

// attachments makes t's colour view (unless it has one: localImage made a read-back target's),
// its depth image and its framebuffer.
static int attachments(s3d *s, s3d_target *t, VkFormat f, char *err, int errlen) {
	if (!t->view) {
		VkImageViewCreateInfo vci = {
			.sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
			.image = t->img,
			.viewType = VK_IMAGE_VIEW_TYPE_2D,
			.format = f,
			.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
		};
		CHECK(vkCreateImageView(s->dev, &vci, NULL, &t->view), "vkCreateImageView (target)");
	}
	if (localImage(s, t->w, t->h, VK_FORMAT_D32_SFLOAT, VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT, VK_IMAGE_ASPECT_DEPTH_BIT,
		&t->depth, &t->depthMem, &t->depthView, err, errlen)) return -1;
	VkImageView views[2] = {t->view, t->depthView};
	VkFramebufferCreateInfo fbi = {
		.sType = VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,
		.renderPass = s->pass,
		.attachmentCount = 2,
		.pAttachments = views,
		.width = (uint32_t)t->w,
		.height = (uint32_t)t->h,
		.layers = 1,
	};
	CHECK(vkCreateFramebuffer(s->dev, &fbi, NULL, &t->fb), "vkCreateFramebuffer");
	return 0;
}

int s3d_target_new(s3d *s, int w, int h, int fmt, const uint64_t *mods, int nmods, s3d_target **out, s3d_export *ex, char *err, int errlen) {
	VkFormat f = vkFormat(fmt);
	if (!s->pipe && pipeline(s, f, err, errlen)) return -1;
	if (s->colour != f) return fail(err, errlen, "a target's format differs from the first target's");
	s3d_target *t = calloc(1, sizeof *t);
	*out = t;
	t->w = w, t->h = h, t->fd = -1;

	VkImageDrmFormatModifierListCreateInfoEXT modList = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_DRM_FORMAT_MODIFIER_LIST_CREATE_INFO_EXT,
		.drmFormatModifierCount = (uint32_t)nmods,
		.pDrmFormatModifiers = mods,
	};
	VkExternalMemoryImageCreateInfo ext = {
		.sType = VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO,
		.pNext = s->modifiers ? &modList : NULL,
		.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT,
	};
	VkImageCreateInfo ici = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
		.pNext = &ext,
		.imageType = VK_IMAGE_TYPE_2D,
		.format = f,
		.extent = {(uint32_t)w, (uint32_t)h, 1},
		.mipLevels = 1,
		.arrayLayers = 1,
		.samples = VK_SAMPLE_COUNT_1_BIT,
		.tiling = s->modifiers ? VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT : VK_IMAGE_TILING_LINEAR,
		.usage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE,
		.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
	};
	CHECK(vkCreateImage(s->dev, &ici, NULL, &t->img), "vkCreateImage (export)");
	VkMemoryRequirements req;
	vkGetImageMemoryRequirements(s->dev, t->img, &req);
	int mt = memType(s, req.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
	if (mt < 0) return fail(err, errlen, "no memory type for an exported image");
	VkMemoryDedicatedAllocateInfo dedicated = {.sType = VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO, .image = t->img};
	VkExportMemoryAllocateInfo exp = {
		.sType = VK_STRUCTURE_TYPE_EXPORT_MEMORY_ALLOCATE_INFO,
		.pNext = &dedicated,
		.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT,
	};
	VkMemoryAllocateInfo mai = {
		.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
		.pNext = &exp,
		.allocationSize = req.size,
		.memoryTypeIndex = (uint32_t)mt,
	};
	CHECK(vkAllocateMemory(s->dev, &mai, NULL, &t->mem), "vkAllocateMemory (export)");
	CHECK(vkBindImageMemory(s->dev, t->img, t->mem, 0), "vkBindImageMemory (export)");

	VkImageSubresource sub = {.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT};
	ex->modifier = 0;
	if (s->modifiers) {
		VkImageDrmFormatModifierPropertiesEXT mp = {.sType = VK_STRUCTURE_TYPE_IMAGE_DRM_FORMAT_MODIFIER_PROPERTIES_EXT};
		CHECK(s->getModifierProps(s->dev, t->img, &mp), "vkGetImageDrmFormatModifierPropertiesEXT");
		ex->modifier = mp.drmFormatModifier;
		sub.aspectMask = VK_IMAGE_ASPECT_MEMORY_PLANE_0_BIT_EXT;
	}
	VkSubresourceLayout lay;
	vkGetImageSubresourceLayout(s->dev, t->img, &sub, &lay);
	ex->offset = (uint32_t)lay.offset;
	ex->pitch = (uint32_t)lay.rowPitch;
	VkMemoryGetFdInfoKHR gfi = {
		.sType = VK_STRUCTURE_TYPE_MEMORY_GET_FD_INFO_KHR,
		.memory = t->mem,
		.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT,
	};
	CHECK(s->getMemoryFd(s->dev, &gfi, &t->fd), "vkGetMemoryFdKHR");
	ex->fd = t->fd;

	t->exported = 1;
	return attachments(s, t, f, err, errlen);
}

int s3d_readback_new(s3d *s, int w, int h, int fmt, s3d_target **out, char *err, int errlen) {
	VkFormat f = vkFormat(fmt);
	if (!s->pipe && pipeline(s, f, err, errlen)) return -1;
	if (s->colour != f) return fail(err, errlen, "a target's format differs from the first target's");
	s3d_target *t = calloc(1, sizeof *t);
	*out = t;
	t->w = w, t->h = h, t->fd = -1;
	// the colour image is an ordinary local one; its copy into readBuf is what the host reads
	if (localImage(s, w, h, f, VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT, VK_IMAGE_ASPECT_COLOR_BIT,
		&t->img, &t->mem, &t->view, err, errlen)) return -1;

	VkBufferCreateInfo bci = {
		.sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
		.size = (VkDeviceSize)w * (VkDeviceSize)h * 4,
		.usage = VK_BUFFER_USAGE_TRANSFER_DST_BIT,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE,
	};
	CHECK(vkCreateBuffer(s->dev, &bci, NULL, &t->readBuf), "vkCreateBuffer (read-back)");
	VkMemoryRequirements breq;
	vkGetBufferMemoryRequirements(s->dev, t->readBuf, &breq);
	// The CPU reads every byte of this buffer each frame, so it wants CACHED host memory: an
	// uncached (write-combined) mapping reads at a few hundred MB/s, about 20 ms for a card-sized
	// frame. Coherent is preferred; a non-coherent one is invalidated before each read.
	int bt = -1;
	VkMemoryPropertyFlags prefs[] = {
		VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_CACHED_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
		VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_CACHED_BIT,
		VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
	};
	for (int i = 0; i < 3 && bt < 0; i++) {
		for (uint32_t k = 0; k < s->mem.memoryTypeCount; k++) {
			if ((breq.memoryTypeBits & (1u << k)) && (s->mem.memoryTypes[k].propertyFlags & prefs[i]) == prefs[i]) {
				bt = (int)k;
				break;
			}
		}
	}
	if (bt < 0) return fail(err, errlen, "no host-visible memory for the read-back buffer");
	t->readCoherent = (s->mem.memoryTypes[bt].propertyFlags & VK_MEMORY_PROPERTY_HOST_COHERENT_BIT) != 0;
	VkMemoryAllocateInfo bai = {.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, .allocationSize = breq.size, .memoryTypeIndex = (uint32_t)bt};
	CHECK(vkAllocateMemory(s->dev, &bai, NULL, &t->readMem), "vkAllocateMemory (read-back buffer)");
	CHECK(vkBindBufferMemory(s->dev, t->readBuf, t->readMem, 0), "vkBindBufferMemory (read-back)");
	CHECK(vkMapMemory(s->dev, t->readMem, 0, VK_WHOLE_SIZE, 0, &t->readMap), "vkMapMemory (read-back)");
	t->offset = 0;
	t->pitch = (VkDeviceSize)w * 4;
	return attachments(s, t, f, err, errlen);
}

int s3d_read(s3d *s, s3d_target *t, uint8_t *dst, char *err, int errlen) {
	if (t->exported || !t->readMap) return fail(err, errlen, "an exported target is the window's to read");
	if (!t->readCoherent) {
		VkMappedMemoryRange r = {.sType = VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE, .memory = t->readMem, .offset = 0, .size = VK_WHOLE_SIZE};
		CHECK(vkInvalidateMappedMemoryRanges(s->dev, 1, &r), "vkInvalidateMappedMemoryRanges (read-back)");
	}
	memcpy(dst, t->readMap, (size_t)t->w * (size_t)t->h * 4);
	return 0;
}

struct draw {
	s3d_target *t;
	const float *draws;
};

static void recDraw(s3d *s, VkCommandBuffer cb, void *arg) {
	struct draw *d = arg;
	s3d_target *t = d->t;
	VkClearValue clear[2] = {
		{.color = {{0x1a / 255.0f, 0x1d / 255.0f, 0x2b / 255.0f, 1}}}, // clearColour in scene.go
		{.depthStencil = {1, 0}},
	};
	VkRenderPassBeginInfo rbi = {
		.sType = VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,
		.renderPass = s->pass,
		.framebuffer = t->fb,
		.renderArea = {{0, 0}, {(uint32_t)t->w, (uint32_t)t->h}},
		.clearValueCount = 2,
		.pClearValues = clear,
	};
	vkCmdBeginRenderPass(cb, &rbi, VK_SUBPASS_CONTENTS_INLINE);
	vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, s->pipe);
	// Clip y points up and the image's rows run down: the viewport flips it (Vulkan 1.1).
	VkViewport vp = {0, (float)t->h, (float)t->w, -(float)t->h, 0, 1};
	VkRect2D sc = {{0, 0}, {(uint32_t)t->w, (uint32_t)t->h}};
	vkCmdSetViewport(cb, 0, 1, &vp);
	vkCmdSetScissor(cb, 0, 1, &sc);
	VkDeviceSize off = 0;
	vkCmdBindVertexBuffers(cb, 0, 1, &s->vbuf, &off);
	vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, s->layout, 0, 1, &s->set, 0, NULL);
	vkCmdPushConstants(cb, s->layout, VK_SHADER_STAGE_VERTEX_BIT, 0, 128, d->draws);
	vkCmdDraw(cb, s->floorCount, 1, s->floorFirst, 0);
	vkCmdPushConstants(cb, s->layout, VK_SHADER_STAGE_VERTEX_BIT, 0, 128, d->draws + 32);
	vkCmdDraw(cb, s->crateCount, 1, s->crateFirst, 0);
	vkCmdEndRenderPass(cb);
	if (t->exported) {
		// Hand the image to the window: the release half of the ownership transfer its importer
		// acquires (from the foreign family, in GENERAL).
		VkImageMemoryBarrier release = {
			.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,
			.srcAccessMask = VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,
			.dstAccessMask = 0,
			.oldLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
			.newLayout = VK_IMAGE_LAYOUT_GENERAL,
			.srcQueueFamilyIndex = s->family,
			.dstQueueFamilyIndex = s->foreign ? VK_QUEUE_FAMILY_FOREIGN_EXT : VK_QUEUE_FAMILY_EXTERNAL,
			.image = t->img,
			.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
		};
		vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT,
			0, 0, NULL, 0, NULL, 1, &release);
		return;
	}
	// A read-back target: copy the image into its host-visible buffer, which the host then reads.
	VkImageMemoryBarrier toCopy = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,
		.srcAccessMask = VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,
		.dstAccessMask = VK_ACCESS_TRANSFER_READ_BIT,
		.oldLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
		.newLayout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
		.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.image = t->img,
		.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
	};
	vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT,
		0, 0, NULL, 0, NULL, 1, &toCopy);
	VkBufferImageCopy region = {
		.bufferOffset = 0,
		.bufferRowLength = 0, // tightly packed
		.bufferImageHeight = 0,
		.imageSubresource = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 0, 1},
		.imageOffset = {0, 0, 0},
		.imageExtent = {(uint32_t)t->w, (uint32_t)t->h, 1},
	};
	vkCmdCopyImageToBuffer(cb, t->img, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, t->readBuf, 1, &region);
	VkBufferMemoryBarrier toHost = {
		.sType = VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER,
		.srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT,
		.dstAccessMask = VK_ACCESS_HOST_READ_BIT,
		.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.buffer = t->readBuf,
		.offset = 0,
		.size = VK_WHOLE_SIZE,
	};
	vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_HOST_BIT,
		0, 0, NULL, 1, &toHost, 0, NULL);
}

int s3d_render(s3d *s, s3d_target *t, const float *draws, char *err, int errlen) {
	struct draw d = {t, draws};
	return oneShot(s, recDraw, &d, err, errlen);
}

void s3d_target_free(s3d *s, s3d_target *t) {
	if (!t) return;
	if (t->fb) vkDestroyFramebuffer(s->dev, t->fb, NULL);
	if (t->depthView) vkDestroyImageView(s->dev, t->depthView, NULL);
	if (t->depth) vkDestroyImage(s->dev, t->depth, NULL);
	if (t->depthMem) vkFreeMemory(s->dev, t->depthMem, NULL);
	if (t->view) vkDestroyImageView(s->dev, t->view, NULL);
	if (t->img) vkDestroyImage(s->dev, t->img, NULL);
	if (t->mem) vkFreeMemory(s->dev, t->mem, NULL);
	if (t->readMap) vkUnmapMemory(s->dev, t->readMem);
	if (t->readBuf) vkDestroyBuffer(s->dev, t->readBuf, NULL);
	if (t->readMem) vkFreeMemory(s->dev, t->readMem, NULL);
	if (t->fd >= 0) close(t->fd);
	free(t);
}

void s3d_free(s3d *s) {
	if (!s) return;
	if (s->dev) {
		vkDeviceWaitIdle(s->dev);
		if (s->pipe) vkDestroyPipeline(s->dev, s->pipe, NULL);
		if (s->pass) vkDestroyRenderPass(s->dev, s->pass, NULL);
		if (s->vs) vkDestroyShaderModule(s->dev, s->vs, NULL);
		if (s->fs) vkDestroyShaderModule(s->dev, s->fs, NULL);
		if (s->layout) vkDestroyPipelineLayout(s->dev, s->layout, NULL);
		if (s->descPool) vkDestroyDescriptorPool(s->dev, s->descPool, NULL);
		if (s->setLayout) vkDestroyDescriptorSetLayout(s->dev, s->setLayout, NULL);
		if (s->sampler) vkDestroySampler(s->dev, s->sampler, NULL);
		if (s->atlasView) vkDestroyImageView(s->dev, s->atlasView, NULL);
		if (s->atlas) vkDestroyImage(s->dev, s->atlas, NULL);
		if (s->atlasMem) vkFreeMemory(s->dev, s->atlasMem, NULL);
		if (s->vbuf) vkDestroyBuffer(s->dev, s->vbuf, NULL);
		if (s->vmem) vkFreeMemory(s->dev, s->vmem, NULL);
		if (s->fence) vkDestroyFence(s->dev, s->fence, NULL);
		if (s->pool) vkDestroyCommandPool(s->dev, s->pool, NULL);
		vkDestroyDevice(s->dev, NULL);
	}
	if (s->inst) vkDestroyInstance(s->inst, NULL);
	free(s);
}
