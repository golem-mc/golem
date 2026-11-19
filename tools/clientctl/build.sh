#!/bin/sh
# Builds the clientctl java agent into reference-server/, which is not
# committed: the jar is built against the downloaded client's own classes and
# has no life outside it.
#
#   mise run client:ctl-build
#
# Plain javac against client.jar and the client's libraries - no mod loader,
# no Gradle - and the jdk's own jar tool for the manifest. Java comes from
# mise: run this through the task, which declares it.

set -e
cd "$(dirname "$0")/../.."

classes=reference-server/clientctl-classes
jar=reference-server/clientctl-agent.jar
client_jar=reference-server/client/client.jar

if [ ! -f "$client_jar" ]; then
    echo "no client jar at $client_jar - run 'mise run client:setup' first" >&2
    exit 1
fi

# The client's libraries ride the classpath the way the client itself runs
# with, so any class the agent touches resolves the way it does in the game.
classpath=$client_jar
for lib in $(find reference-server/client/libraries -name '*.jar' | sort); do
    classpath=$classpath:$lib
done

rm -rf "$classes"
mkdir -p "$classes"
sources=$(find tools/clientctl/src/main/java -name '*.java')
javac -Xlint:-options -cp "$classpath" -d "$classes" $sources

cat <<EOF > "$classes/manifest.txt"
Premain-Class: dev.golem.clientctl.ClientCtlAgent
EOF

rm -f "$jar"
jar cfm "$jar" "$classes/manifest.txt" -C "$classes" dev
rm -f "$classes/manifest.txt"
echo "built $jar"
