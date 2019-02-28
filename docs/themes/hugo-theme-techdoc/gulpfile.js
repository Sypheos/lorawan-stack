//  Copyright © 2019 The Things Network Foundation, The Things Industries B.V.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

'use strict';

var gulp = require('gulp');
var $ = require('gulp-load-plugins')();

require('es6-promise').polyfill();

var runSequence = require('run-sequence');

var browserSync = require('browser-sync').create();
var reload = browserSync.reload;

var src_paths = {
  sass: ['src/scss/*.scss'],
  script: [
    'static/js/*.js',
    '!static/js/*.min.js'
  ],
};

var dest_paths = {
  style: 'static/css/',
  script: 'static/js/',
  browserSync: ''
};


gulp.task('lint:sass', function() {
  return gulp.src(src_paths.sass)
    .pipe($.plumber({
      errorHandler: function(err) {
        console.log(err.messageFormatted);
        this.emit('end');
      }
    }))
    .pipe($.stylelint({
      config: {
        ignoreFiles: "src/scss/_normalize.scss",
        extends: [
          "stylelint-config-recommended",
          "stylelint-scss",
          "stylelint-config-recommended-scss"
        ],
        rules: {
          "block-no-empty": null,
          "no-descending-specificity": null
        }
      },
      reporters: [
        { formatter: 'string', console: true }
      ]
    }));
});

gulp.task('sass:style', function() {
  return gulp.src(src_paths.sass)
    .pipe($.plumber({
      errorHandler: function(err) {
        console.log(err.messageFormatted);
        this.emit('end');
      }
    }))
    .pipe($.sass({
      outputStyle: 'expanded'
    }).on( 'error', $.sass.logError ) )
    .pipe($.autoprefixer({
        browsers: ['last 2 versions'],
        cascade: false
    }))
    .pipe(gulp.dest(dest_paths.style))
    .pipe($.cssnano())
    .pipe($.rename({ suffix: '.min' }))
    .pipe(gulp.dest(dest_paths.style));
});

gulp.task('javascript', function() {
  return gulp.src(src_paths.script)
    .pipe($.uglify().on('error', $.util.log))
    .pipe($.rename({ suffix: '.min' }))
    .pipe(gulp.dest(dest_paths.script));
});

gulp.task('lint:javascript', function() {
  return gulp.src(src_paths.script)
    .pipe($.jshint())
    .pipe($.jshint.reporter('jshint-stylish'));
});

gulp.task('browser-sync', function() {
  browserSync.init({
    server: {
      baseDir: dest_paths.browserSync
    }
  });

  gulp.watch(src_paths.sass, ['default']).on('change', reload);
});

gulp.task('lint', ['lint:sass', 'lint:javascript']);
gulp.task('sass', ['sass:style']);
gulp.task('script', ['javascript']);
gulp.task('serve', ['browser-sync']);

gulp.task('default', function(callback) {
  runSequence(
    'lint',
    'sass',
    'script',
    callback
  );
});

gulp.task('watch', function() {
  gulp.watch([src_paths.sass, src_paths.script], ['default']);
});
