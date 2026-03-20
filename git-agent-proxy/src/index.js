const AI_GATEWAY_URL =
  "https://gateway.ai.cloudflare.com/v1/{account_id}/{gateway_id}/openai";

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

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

    const allowedModels = (env.ALLOWED_MODELS ?? "gpt-4o-mini")
      .split(",")
      .map((m) => m.trim());

    if (!allowedModels.includes(body.model)) {
      return new Response(
        JSON.stringify({ error: `model not allowed: ${body.model}` }),
        { status: 422, headers: { "Content-Type": "application/json" } }
      );
    }

    if (body.max_completion_tokens == null || body.max_completion_tokens > 4096) {
      body.max_completion_tokens = 4096;
    }

    const upstream = new Request(AI_GATEWAY_URL + "/chat/completions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${env.OPENAI_API_KEY}`,
        "cf-aig-authorization": `Bearer ${env.CF_AIG_TOKEN}`,
      },
      body: JSON.stringify(body),
    });

    return fetch(upstream);
  },
};
