export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const aiGatewayURL = `https://gateway.ai.cloudflare.com/v1/${env.CF_ACCOUNT_ID}/${env.CF_GATEWAY_ID}/compat`;

    if (url.pathname !== "/chat/completions") {
      return new Response("Not Found", { status: 404 });
    }

    if (request.method !== "POST") {
      return new Response("Method Not Allowed", { status: 405 });
    }

    const authHeader = request.headers.get("Authorization") ?? "";
    const clientToken = authHeader.startsWith("Bearer ")
      ? authHeader.slice(7)
      : "";

    if (!clientToken || clientToken !== env.CLIENT_TOKEN) {
      return new Response("Unauthorized", { status: 401 });
    }

    let body;
    try {
      body = await request.json();
    } catch {
      return new Response("Bad Request", { status: 400 });
    }

    if (env.ALLOWED_SYSTEM_PROMPTS) {
      const allowedPrefixes = env.ALLOWED_SYSTEM_PROMPTS
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean);
      const systemMsg = (body.messages ?? []).find((m) => m.role === "system");
      const systemContent =
        typeof systemMsg?.content === "string" ? systemMsg.content : "";
      const allowed = allowedPrefixes.some((prefix) =>
        systemContent.startsWith(prefix)
      );
      if (!allowed) {
        return new Response("Forbidden", { status: 403 });
      }
    }

    body.model = env.MODEL;

    if (body.max_completion_tokens == null || body.max_completion_tokens > 4096) {
      body.max_completion_tokens = 4096;
    }

    const bodyStr = JSON.stringify(body);
    if (bodyStr.length > 512 * 1024) {
      return new Response("Payload Too Large", { status: 413 });
    }

    const upstream = new Request(aiGatewayURL + "/chat/completions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "cf-aig-authorization": `Bearer ${env.CF_AIG_TOKEN}`,
      },
      body: bodyStr,
    });

    const response = await fetch(upstream);

    return body.stream ? maskModelInStream(response) : maskModelInJson(response);
  },
};

async function maskModelInJson(response) {
  const data = await response.json();
  if (data.model != null) {
    data.model = "git-agent";
  }
  return new Response(JSON.stringify(data), {
    status: response.status,
    headers: { "Content-Type": "application/json" },
  });
}

function maskModelInStream(response) {
  const { readable, writable } = new TransformStream({
    transform(chunk, controller) {
      const text = new TextDecoder().decode(chunk);
      const masked = text.replace(/"model":"[^"]*"/g, '"model":"git-agent"');
      controller.enqueue(new TextEncoder().encode(masked));
    },
  });
  response.body.pipeTo(writable);
  return new Response(readable, {
    status: response.status,
    headers: response.headers,
  });
}
