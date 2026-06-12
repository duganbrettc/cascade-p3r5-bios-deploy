FROM nginx:alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY *.html /usr/share/nginx/html/
COPY *.css /usr/share/nginx/html/
COPY *.js /usr/share/nginx/html/

RUN rm -f /usr/share/nginx/html/50x.html

EXPOSE 80
