// Regenerate: mise exec java@temurin-25 -- java server/chatsecurity/testdata/SignedFixture.java > server/chatsecurity/testdata/signed.json
// Independent JCA fixture for ProfilePublicKey.Data.signedPayload and
// PlayerChatMessage.updateSignature; no Go signing implementation involved.
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.*;
import java.util.*;

class SignedFixture {
    static byte[] sign(String algorithm, PrivateKey key, byte[] body) throws Exception {
        var signer = Signature.getInstance(algorithm);
        signer.initSign(key);
        signer.update(body);
        return signer.sign();
    }
    static String field(String name, byte[] value) {
        return "\"" + name + "\":\"" + Base64.getEncoder().encodeToString(value) + "\"";
    }
    public static void main(String[] args) throws Exception {
        var generator = KeyPairGenerator.getInstance("RSA");
        generator.initialize(2048);
        var service = generator.generateKeyPair();
        var player = generator.generateKeyPair();
        var profile = UUID.fromString("00112233-4455-6677-8899-aabbccddeeff");
        var session = UUID.fromString("ffeeddcc-bbaa-9988-7766-554433221100");
        long timestamp = 1900000000123L, expiry = timestamp + 3600000L;
        var certificateBytes = new ByteArrayOutputStream();
        var certificate = new DataOutputStream(certificateBytes);
        certificate.writeLong(profile.getMostSignificantBits());
        certificate.writeLong(profile.getLeastSignificantBits());
        certificate.writeLong(expiry);
        certificate.write(player.getPublic().getEncoded());
        var bytes = new ByteArrayOutputStream();
        var out = new DataOutputStream(bytes);
        out.writeInt(1);
        for (var id : List.of(profile, session)) {
            out.writeLong(id.getMostSignificantBits()); out.writeLong(id.getLeastSignificantBits());
        }
        out.writeInt(0); out.writeLong(-123456789L); out.writeLong(timestamp / 1000);
        var content = "héllo  世界".getBytes(StandardCharsets.UTF_8);
        out.writeInt(content.length); out.write(content);
        byte[] seen = new byte[256]; seen[0] = (byte)255;
        out.writeInt(1); out.write(seen);
        System.out.println("{" + field("service", service.getPublic().getEncoded()) + ","
            + field("player", player.getPublic().getEncoded()) + ","
            + field("certificate", sign("SHA1withRSA", service.getPrivate(), certificateBytes.toByteArray())) + ","
            + field("payload", bytes.toByteArray()) + ","
            + field("signature", sign("SHA256withRSA", player.getPrivate(), bytes.toByteArray()))
            + ",\"checksum\":" + (byte)(31 + Arrays.hashCode(seen)) + "}");
    }
}
