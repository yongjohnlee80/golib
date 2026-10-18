#version 450
// The scene's vertex shader (scene.go draws the same in Go): position by mvp, and the face's
// light from its normal turned by model.

layout(location = 0) in vec3 pos;
layout(location = 1) in vec3 normal;
layout(location = 2) in vec2 uv;

layout(push_constant) uniform Draw {
	mat4 mvp;
	mat4 model;
} draw;

layout(location = 0) out vec2 vUV;
layout(location = 1) out float vLight;

const vec3 lightDir = normalize(vec3(0.4, 0.8, 0.45));
const float ambient = 0.35;

void main() {
	gl_Position = draw.mvp * vec4(pos, 1.0);
	vec3 n = (draw.model * vec4(normal, 0.0)).xyz;
	vLight = ambient + (1.0 - ambient) * max(0.0, dot(n, lightDir));
	vUV = uv;
}
