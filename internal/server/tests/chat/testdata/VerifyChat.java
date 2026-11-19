// Run after CHAT_TRANSCRIPT=<file> mise exec -- go test ./server/tests/chat -count=1 -run
// 'TestVerifiedMessageCommands|TestSignedSessionsInterleavedRenewalAndLateJoin':
// java -cp '<server jar>:<library jars>' VerifyChat.java <file>
// Uses vanilla's client validator, including duplicate delivery/chain rules.
import java.nio.file.*;
import java.nio.charset.StandardCharsets;
import java.security.*;
import java.security.spec.X509EncodedKeySpec;
import java.time.Instant;
import java.util.*;
import net.minecraft.network.chat.*;
import net.minecraft.util.SignatureValidator;

class VerifyChat {
    public static void main(String[] args) throws Exception {
        var validators = new HashMap<String, SignedMessageValidator>();
        var globalIndexes = new HashMap<String, Integer>();
        var decode = Base64.getDecoder();
        int count = 0;
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            var row = line.split("\t", -1);
            String recipient = row[0];
            int global = Integer.parseInt(row[1]);
            if (global != globalIndexes.getOrDefault(recipient, 0)) throw new AssertionError("global index " + global);
            globalIndexes.put(recipient, global + 1);
            var sender = UUID.fromString(row[2]);
            var session = UUID.fromString(row[3]);
            String identity = recipient + "/" + sender + "/" + session;
            var key = KeyFactory.getInstance("RSA").generatePublic(new X509EncodedKeySpec(decode.decode(row[7])));
            var validator = validators.computeIfAbsent(identity, ignored -> new SignedMessageValidator.KeyBased(
                SignatureValidator.from(key, "SHA256withRSA"), () -> false));
            var signatures = new ArrayList<MessageSignature>();
            byte[] seen = decode.decode(row[10]);
            for (int i = 0; i < seen.length; i += 256) signatures.add(new MessageSignature(Arrays.copyOfRange(seen, i, i + 256)));
            var body = new SignedMessageBody(new String(decode.decode(row[9]), StandardCharsets.UTF_8),
                Instant.ofEpochMilli(Long.parseLong(row[5])), Long.parseLong(row[6]), new LastSeenMessages(signatures));
            var message = new PlayerChatMessage(new SignedMessageLink(Integer.parseInt(row[4]), sender, session),
                new MessageSignature(decode.decode(row[8])), body, null, FilterMask.PASS_THROUGH);
            if (validator.updateAndValidate(message) == null) throw new AssertionError("vanilla refused line " + (count + 1));
            count++;
        }
        if (count == 0) throw new AssertionError("empty transcript");
        System.out.println("Vanilla client validator accepted " + count + " received messages");
    }
}
