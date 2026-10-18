#version 450
// The scene's fragment shader: the atlas texel, lit.

layout(set = 0, binding = 0) uniform sampler2D atlas;

layout(location = 0) in vec2 vUV;
layout(location = 1) in float vLight;

layout(location = 0) out vec4 colour;

void main() {
	colour = vec4(texture(atlas, vUV).rgb * vLight, 1.0);
}
