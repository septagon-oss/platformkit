# Build from the repository root. This produces a generic editor, not a Collect
# document, a published image or a staging deployment.
FROM oven/bun:1.3.10@sha256:b86c67b531d87b4db11470d9b2bd0c519b1976eee6fcd71634e73abfa6230d2e AS bun
FROM node:24-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e AS build
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
COPY tools/designexport/openpencil/Dockerfile tools/designexport/openpencil/nginx.conf LICENSE NOTICE ./
WORKDIR /upstream
RUN node /adapter/build-editor.mjs /upstream

FROM nginxinc/nginx-unprivileged:1.29-alpine@sha256:0c79d56aee561a1d81c63f00eee5fb5fe29279560cdc55e91425133104c7fbe6
COPY --from=build /upstream/dist/ /usr/share/nginx/html/
COPY tools/designexport/openpencil/nginx.conf /etc/nginx/conf.d/default.conf
USER 101:101
EXPOSE 8080
