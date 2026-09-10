# Build from the repository root. This produces a generic editor, not a Collect
# document, a published image or a staging deployment.
FROM oven/bun:1.3.10@sha256:b86c67b531d87b4db11470d9b2bd0c519b1976eee6fcd71634e73abfa6230d2e AS bun
FROM node:24-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e AS build-inputs
COPY --from=bun /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /upstream
ADD --checksum=sha256:68f86e1f807f35847b9453a7049b3d59375c4eef441a4afffc215a24c843a409 https://codeload.github.com/open-pencil/open-pencil/tar.gz/c29654cd07ac46b53e76c16b18505919f16571be /tmp/upstream.tar.gz
RUN tar -xzf /tmp/upstream.tar.gz --strip-components=1 && rm /tmp/upstream.tar.gz
# These exact licenses correspond to the bundled Inter 4.001 and Noto 2.021
# font metadata. The archive has the fonts but omits their complete licenses.
ADD --chmod=0644 --checksum=sha256:262481e844521b326f5ecd053e59b98c8b2da78c8ee1bdbb6e8174305e54935a https://raw.githubusercontent.com/rsms/inter/66647c0bbbe41a850d79d9c76fb13add3378940f/LICENSE.txt /upstream/public/licenses/Inter-OFL.txt
ADD --chmod=0644 --checksum=sha256:a7a5a25eb188bf1cd96982030d53e23c33485c69b1044a562254226857ee13af https://raw.githubusercontent.com/notofonts/arabic/59f5a3fd985bf24858915c3dddfc51a537640965/OFL.txt /upstream/public/licenses/NotoNaskhArabic-OFL.txt
RUN bun install --frozen-lockfile --ignore-scripts
WORKDIR /adapter
COPY tools/designexport/openpencil/package.json tools/designexport/openpencil/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY tools/designexport/openpencil/*-correction.mjs tools/designexport/openpencil/corrections.mjs tools/designexport/openpencil/build-editor.mjs ./
COPY tools/designexport/openpencil/editor-fonts.mjs tools/designexport/openpencil/fonts.mjs tools/designexport/openpencil/source-positioning.mjs tools/designexport/openpencil/source-box.mjs ./
COPY tools/designexport/openpencil/variable-color.mjs tools/designexport/openpencil/color-expression.mjs tools/designexport/openpencil/computed-color.mjs ./
COPY tools/designexport/openpencil/Dockerfile tools/designexport/openpencil/nginx.conf LICENSE NOTICE ./
WORKDIR /upstream
# These published packages omit their license files. The Yoga revision is the
# package's gitHead. Provider-utils 4.0.33 uses its tagged root notice.
ADD --chmod=0644 --checksum=sha256:52412d7bc7ce4157ea628bbaacb8829e0a9cb3c58f57f99176126bc8cf2bfc85 https://raw.githubusercontent.com/open-pencil/yoga/d96884e7105fdef998a64a89f1d3cf775eb38efd/LICENSE /upstream/public/licenses/Yoga-LICENSE
ADD --chmod=0644 --checksum=sha256:b4f9adb7c568904834d0dd6cc98d16c390d21ca32fc17ae7a267715269bd5529 https://raw.githubusercontent.com/vercel/ai/a2d8be2aa0c024f1e3d275d94b67cc17c9159043/LICENSE /upstream/public/licenses/AI-SDK-ProviderUtils-LICENSE
# format 0.2.2 names MIT in its manifest and README; its maintainer supplied
# the complete license file in this later, attribution-only source revision.
ADD --chmod=0644 --checksum=sha256:0b2c94863590ca2aed327e89642b7e74b1608ec423bfec1d8f1beba2945fc4ba https://raw.githubusercontent.com/samsonjs/format/91b6bd78af9b061c90010b86d83caa051edeb1ea/License.md /upstream/public/licenses/format-LICENSE
# The Unicode trie runtime omits its root license from npm. Keep that exact
# revision's notice and the Unicode data notice beside the bundled line breaker.
ADD --chmod=0644 --checksum=sha256:e59138ecbc0b770010b0781905e2bcc181e4f0735494c58fd9681b24f0246187 https://raw.githubusercontent.com/cto-af/unicode-trie/e82d02e5c6f6e5d6e6d66bd75ac0bb2cb397abef/LICENSE /upstream/public/licenses/UnicodeTrie-LICENSE
ADD --chmod=0644 --checksum=sha256:e7a93b009565cfce55919a381437ac4db883e9da2126fa28b91d12732bc53d96 https://www.unicode.org/license.txt /upstream/public/licenses/Unicode-LICENSE
FROM build-inputs AS build
RUN node /adapter/build-editor.mjs /upstream

FROM build-inputs AS preview-build
# An explicit verification profile, not a generic-editor or client-brand default.
RUN node /adapter/build-editor.mjs /upstream \
    --font 'IBM Plex Sans' 400 normal /adapter/node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-400-normal.woff \
    --font 'IBM Plex Sans' 500 normal /adapter/node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-500-normal.woff \
    --font 'IBM Plex Sans' 600 normal /adapter/node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-600-normal.woff \
    --font 'IBM Plex Sans' 700 normal /adapter/node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-700-normal.woff \
    --font-license /adapter/node_modules/@fontsource/ibm-plex-sans/LICENSE

FROM nginxinc/nginx-unprivileged:1.29-alpine@sha256:0c79d56aee561a1d81c63f00eee5fb5fe29279560cdc55e91425133104c7fbe6 AS runtime
COPY tools/designexport/openpencil/nginx.conf /etc/nginx/conf.d/default.conf
USER 101:101
EXPOSE 8080

FROM runtime AS preview
COPY --from=preview-build /upstream/dist/ /usr/share/nginx/html/

FROM runtime AS editor
COPY --from=build /upstream/dist/ /usr/share/nginx/html/
