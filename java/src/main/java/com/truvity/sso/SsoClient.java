package com.truvity.sso;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.nimbusds.jose.JOSEException;
import com.nimbusds.jose.JWSAlgorithm;
import com.nimbusds.jose.JWSHeader;
import com.nimbusds.jose.crypto.RSASSASigner;
import com.nimbusds.jwt.JWTClaimsSet;
import com.nimbusds.jwt.SignedJWT;
import org.bouncycastle.asn1.pkcs.PrivateKeyInfo;
import org.bouncycastle.openssl.PEMKeyPair;
import org.bouncycastle.openssl.PEMParser;
import org.bouncycastle.openssl.jcajce.JcaPEMKeyConverter;

import java.io.IOException;
import java.io.StringReader;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.security.PrivateKey;
import java.time.Instant;
import java.util.Date;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;

/**
 * Java client for the SSO administration API: every operation is scoped to
 * one product, authenticated with the product's machine key. Tokens are
 * minted (JWT profile) and refreshed automatically. Thread-safe.
 */
public final class SsoClient {

    /** Per-product connection settings, issued during product onboarding. */
    public record Config(String endpoint, String product, String issuer, String projectId, String keyJson) {}

    private static final ObjectMapper JSON = new ObjectMapper();

    private final String endpoint;
    private final String product;
    private final String issuer;
    private final String scope;
    private final String keyId;
    private final String keyUserId;
    private final PrivateKey privateKey;
    private final HttpClient http;

    private final Object tokenLock = new Object();
    private String token;
    private Instant tokenExpiry = Instant.EPOCH;

    public SsoClient(Config cfg) {
        if (cfg.endpoint() == null || cfg.product() == null || cfg.issuer() == null
                || cfg.projectId() == null || cfg.keyJson() == null) {
            throw new IllegalArgumentException("sso: endpoint, product, issuer, projectId and keyJson are required");
        }
        this.endpoint = cfg.endpoint().replaceAll("/+$", "");
        this.product = cfg.product();
        this.issuer = cfg.issuer().replaceAll("/+$", "");
        this.scope = "openid urn:zitadel:iam:org:project:id:" + cfg.projectId() + ":aud";
        try {
            JsonNode key = JSON.readTree(cfg.keyJson());
            this.keyId = key.get("keyId").asText();
            this.keyUserId = key.get("userId").asText();
            this.privateKey = parsePem(key.get("key").asText());
        } catch (IOException e) {
            throw new IllegalArgumentException("sso: invalid machine key JSON", e);
        }
        this.http = HttpClient.newHttpClient();
    }

    // -- users ---------------------------------------------------------------

    /** Creates the central identity and sends the invitation (409 → onboard). */
    public JsonNode createUser(Map<String, Object> request) {
        return call("POST", "/users", request, true, null);
    }

    /** Marks an EXISTING identity (centralId, or VERIFIED email) as a member. */
    public JsonNode onboard(Map<String, Object> request) {
        return call("POST", "/users/onboard", request, true, null);
    }

    /** One page of the product's onboarded users. */
    public JsonNode listUsers(Map<String, String> params) {
        return call("GET", "/users" + query(params), null, false, null);
    }

    /** One user's detail within the product scope (404 outside it). */
    public JsonNode getUser(String centralId) {
        return call("GET", "/users/" + enc(centralId), null, false, null);
    }

    // -- support operations ----------------------------------------------------

    public JsonNode sendVerificationEmail(String centralId) { return action(centralId, "/verification-email"); }

    /** Removes the user's second factors (GLOBAL effect). */
    public JsonNode resetMfa(String centralId) { return action(centralId, "/mfa/reset"); }

    /** Starts a central password reset (GLOBAL effect). */
    public JsonNode resetPassword(String centralId) { return action(centralId, "/password/reset"); }

    /** Deactivates the central identity (GLOBAL effect). */
    public JsonNode lock(String centralId) { return action(centralId, "/lock"); }

    /** Reactivates a locked identity (GLOBAL effect). */
    public JsonNode unlock(String centralId) { return action(centralId, "/unlock"); }

    // -- sessions ----------------------------------------------------------------

    public JsonNode listSessions(String centralId) {
        return call("GET", "/users/" + enc(centralId) + "/sessions", null, false, null);
    }

    public void terminateSession(String centralId, String sessionId) {
        call("DELETE", "/users/" + enc(centralId) + "/sessions/" + enc(sessionId), null, true, null);
    }

    public void terminateAllSessions(String centralId) {
        call("DELETE", "/users/" + enc(centralId) + "/sessions", null, true, null);
    }

    // -- audit ----------------------------------------------------------------------

    /** The user's audit timeline within the product scope. */
    public JsonNode getAudit(String centralId, Map<String, String> params) {
        return call("GET", "/users/" + enc(centralId) + "/audit" + query(params), null, false, null);
    }

    // -- plumbing ----------------------------------------------------------------------

    private JsonNode action(String centralId, String suffix) {
        return call("POST", "/users/" + enc(centralId) + suffix, null, true, null);
    }

    private String accessToken() {
        synchronized (tokenLock) {
            if (token != null && Instant.now().isBefore(tokenExpiry.minusSeconds(120))) {
                return token;
            }
            try {
                Instant now = Instant.now();
                SignedJWT assertion = new SignedJWT(
                        new JWSHeader.Builder(JWSAlgorithm.RS256).keyID(keyId).build(),
                        new JWTClaimsSet.Builder()
                                .issuer(keyUserId).subject(keyUserId).audience(issuer)
                                .issueTime(Date.from(now)).expirationTime(Date.from(now.plusSeconds(600)))
                                .build());
                assertion.sign(new RSASSASigner(privateKey));

                String form = "grant_type=" + enc("urn:ietf:params:oauth:grant-type:jwt-bearer")
                        + "&scope=" + enc(scope)
                        + "&assertion=" + enc(assertion.serialize());
                HttpResponse<String> resp = http.send(
                        HttpRequest.newBuilder(URI.create(issuer + "/oauth/v2/token"))
                                .header("Content-Type", "application/x-www-form-urlencoded")
                                .POST(HttpRequest.BodyPublishers.ofString(form))
                                .build(),
                        HttpResponse.BodyHandlers.ofString());
                if (resp.statusCode() >= 300) {
                    throw new ApiException(resp.statusCode(), "TokenError", resp.body(), null);
                }
                JsonNode data = JSON.readTree(resp.body());
                token = data.get("access_token").asText();
                tokenExpiry = Instant.now().plusSeconds(data.get("expires_in").asLong());
                return token;
            } catch (JOSEException | IOException e) {
                throw new IllegalStateException("sso: token request failed", e);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IllegalStateException("sso: token request interrupted", e);
            }
        }
    }

    private JsonNode call(String method, String path, Map<String, Object> body, boolean mutation, String idempotencyKey) {
        try {
            HttpRequest.Builder req = HttpRequest.newBuilder(
                            URI.create(endpoint + "/v1/products/" + enc(product) + path))
                    .header("Authorization", "Bearer " + accessToken());
            if (mutation) {
                req.header("Idempotency-Key", idempotencyKey != null ? idempotencyKey : UUID.randomUUID().toString());
            }
            if (body != null) {
                req.header("Content-Type", "application/json")
                        .method(method, HttpRequest.BodyPublishers.ofString(JSON.writeValueAsString(body)));
            } else {
                req.method(method, HttpRequest.BodyPublishers.noBody());
            }
            HttpResponse<String> resp = http.send(req.build(), HttpResponse.BodyHandlers.ofString());
            if (resp.statusCode() >= 300) {
                String title = "";
                String detail = "";
                JsonNode errors = null;
                try {
                    JsonNode problem = JSON.readTree(resp.body());
                    title = problem.path("title").asText();
                    detail = problem.path("detail").asText();
                    errors = problem.get("errors");
                } catch (IOException ignored) {
                    detail = resp.body();
                }
                throw new ApiException(resp.statusCode(), title, detail, errors);
            }
            return resp.body().isEmpty() ? JSON.nullNode() : JSON.readTree(resp.body());
        } catch (IOException e) {
            throw new IllegalStateException("sso: " + method + " " + path + " failed", e);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("sso: " + method + " " + path + " interrupted", e);
        }
    }

    private static PrivateKey parsePem(String pem) throws IOException {
        try (PEMParser parser = new PEMParser(new StringReader(pem))) {
            Object obj = parser.readObject();
            JcaPEMKeyConverter converter = new JcaPEMKeyConverter();
            if (obj instanceof PEMKeyPair pair) { // PKCS#1
                return converter.getKeyPair(pair).getPrivate();
            }
            if (obj instanceof PrivateKeyInfo info) { // PKCS#8
                return converter.getPrivateKey(info);
            }
            throw new IOException("unsupported PEM content: " + (obj == null ? "empty" : obj.getClass().getName()));
        }
    }

    private static String query(Map<String, String> params) {
        if (params == null || params.isEmpty()) {
            return "";
        }
        StringBuilder sb = new StringBuilder("?");
        Map<String, String> copy = new HashMap<>(params);
        copy.forEach((k, v) -> {
            if (sb.length() > 1) sb.append('&');
            sb.append(enc(k)).append('=').append(enc(v));
        });
        return sb.toString();
    }

    private static String enc(String s) {
        return URLEncoder.encode(s, StandardCharsets.UTF_8);
    }

    /** A non-2xx response (RFC 9457 problem shape). */
    public static final class ApiException extends RuntimeException {
        private final int status;
        private final JsonNode errors;

        ApiException(int status, String title, String detail, JsonNode errors) {
            super("sso: " + status + " " + title + ": " + detail);
            this.status = status;
            this.errors = errors;
        }

        public int status() { return status; }

        /**
         * The already-existing identity id from a create-user conflict —
         * use it with onboard() instead. Empty when absent.
         */
        public String existingCentralId() {
            if (status != 409 || errors == null) {
                return "";
            }
            for (JsonNode d : errors) {
                JsonNode v = d.get("value");
                if (v != null && v.isTextual() && !v.asText().isEmpty()) {
                    return v.asText();
                }
            }
            return "";
        }
    }
}
