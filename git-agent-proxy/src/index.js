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

    return fetch(upstream);
  },
};
