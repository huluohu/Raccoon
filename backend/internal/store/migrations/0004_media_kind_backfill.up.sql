-- 0004：媒体库 kind 回填（0003 加列时存量数据统一为 other，这里按 mime/扩展名回填真实类别）
-- 回填后新增文件由应用层按 mime/扩展名直接写入 kind。

UPDATE media_files SET kind = 'video'
  WHERE (mime LIKE 'video/%' OR lower(name) LIKE '%.mp4' OR lower(name) LIKE '%.mkv'
      OR lower(name) LIKE '%.webm' OR lower(name) LIKE '%.mov' OR lower(name) LIKE '%.flv'
      OR lower(name) LIKE '%.m4v' OR lower(name) LIKE '%.avi' OR lower(name) LIKE '%.ts');
UPDATE media_files SET kind = 'audio'
  WHERE kind = 'other' AND (mime LIKE 'audio/%' OR lower(name) LIKE '%.mp3' OR lower(name) LIKE '%.m4a'
      OR lower(name) LIKE '%.flac' OR lower(name) LIKE '%.opus' OR lower(name) LIKE '%.wav'
      OR lower(name) LIKE '%.aac' OR lower(name) LIKE '%.ogg');
UPDATE media_files SET kind = 'image'
  WHERE kind = 'other' AND (mime LIKE 'image/%' OR lower(name) LIKE '%.jpg' OR lower(name) LIKE '%.jpeg'
      OR lower(name) LIKE '%.png' OR lower(name) LIKE '%.gif' OR lower(name) LIKE '%.webp'
      OR lower(name) LIKE '%.avif' OR lower(name) LIKE '%.bmp');
UPDATE media_files SET kind = 'subtitle'
  WHERE kind = 'other' AND (lower(name) LIKE '%.srt' OR lower(name) LIKE '%.vtt' OR lower(name) LIKE '%.ass');
