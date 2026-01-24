-- Migration: Add podcast and video sources
-- Run this AFTER 002_add_media_support.sql

-- Get category IDs for podcasts and videos
DO $$
DECLARE
    podcasts_id UUID;
    videos_id UUID;
BEGIN
    SELECT id INTO podcasts_id FROM categories WHERE slug = 'podcasts';
    SELECT id INTO videos_id FROM categories WHERE slug = 'videos';

    -- Technology Podcasts
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('The Vergecast', 'vergecast', 'https://feeds.megaphone.fm/vergecast', 'https://www.theverge.com/the-vergecast', podcasts_id, true),
        ('Accidental Tech Podcast', 'atp', 'https://atp.fm/episodes?format=rss', 'https://atp.fm', podcasts_id, true),
        ('Darknet Diaries', 'darknet-diaries', 'https://feeds.megaphone.fm/darknetdiaries', 'https://darknetdiaries.com', podcasts_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- News & Politics Podcasts
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('The Daily', 'the-daily', 'https://feeds.simplecast.com/54nAGcIl', 'https://www.nytimes.com/column/the-daily', podcasts_id, true),
        ('Up First', 'up-first', 'https://feeds.npr.org/510318/podcast.xml', 'https://www.npr.org/podcasts/510318/up-first', podcasts_id, true),
        ('Pod Save America', 'pod-save-america', 'https://feeds.megaphone.fm/pod-save-america', 'https://crooked.com/podcast-series/pod-save-america/', podcasts_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science Podcasts
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('Radiolab', 'radiolab', 'https://feeds.simplecast.com/EmVW7VGp', 'https://radiolab.org', podcasts_id, true),
        ('StarTalk Radio', 'startalk', 'https://feeds.simplecast.com/4T39_jAj', 'https://www.startalkradio.net', podcasts_id, true),
        ('Science Vs', 'science-vs', 'https://feeds.megaphone.fm/sciencevs', 'https://gimletmedia.com/shows/science-vs', podcasts_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Technology Videos (YouTube)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('MKBHD', 'mkbhd', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCBJycsmduvYEL83R_U4JriQ', 'https://www.youtube.com/@mkbhd', videos_id, true),
        ('Fireship', 'fireship', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCsBjURrPoezykLs9EqgamOA', 'https://www.youtube.com/@Fireship', videos_id, true),
        ('Linus Tech Tips', 'linus-tech-tips', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCXuqSBlHAE6Xw-yeJA0Tunw', 'https://www.youtube.com/@LinusTechTips', videos_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science Videos (YouTube)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('Veritasium', 'veritasium', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCHnyfMqiRRG1u-2MsSQLbXA', 'https://www.youtube.com/@veritasium', videos_id, true),
        ('Kurzgesagt', 'kurzgesagt', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q', 'https://www.youtube.com/@kurzgesagt', videos_id, true),
        ('SmarterEveryDay', 'smarter-every-day', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC6107grRI4m0o2-emgoDnAA', 'https://www.youtube.com/@smartereveryday', videos_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- News Videos (YouTube)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('Vox', 'vox', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCLXo7UDZvByw2ixzpQCufnA', 'https://www.youtube.com/@Vox', videos_id, true),
        ('PBS NewsHour', 'pbs-newshour', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC6ZFN9Tx6xh-skXCuRHCDpQ', 'https://www.youtube.com/@PBSNewsHour', videos_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Health Podcasts
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('Huberman Lab', 'huberman-lab', 'https://feeds.megaphone.fm/hubermanlab', 'https://www.hubermanlab.com', podcasts_id, true),
        ('The Peter Attia Drive', 'peter-attia', 'https://peterattiamd.com/feed/podcast/', 'https://peterattiamd.com/podcast/', podcasts_id, true),
        ('On Purpose with Jay Shetty', 'on-purpose', 'https://feeds.simplecast.com/A7_FPje2', 'https://jayshetty.me/podcast/', podcasts_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Health Videos (YouTube)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('Doctor Mike', 'doctor-mike', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC0QHWhjbe5fGJEPz3sVb6nw', 'https://www.youtube.com/@DoctorMike', videos_id, true),
        ('Jeff Nippard', 'jeff-nippard', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC68TLK0mAEzUyHx5x5k-S1Q', 'https://www.youtube.com/@JeffNippard', videos_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports Podcasts
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('The Bill Simmons Podcast', 'bill-simmons', 'https://feeds.megaphone.fm/the-bill-simmons-podcast', 'https://www.theringer.com/the-bill-simmons-podcast', podcasts_id, true),
        ('Pardon My Take', 'pardon-my-take', 'https://mcsorleys.barstoolsports.com/feed/pardon-my-take', 'https://www.barstoolsports.com/shows/pardon-my-take', podcasts_id, true),
        ('The Ringer NBA Show', 'ringer-nba', 'https://feeds.megaphone.fm/the-ringer-nba-show', 'https://www.theringer.com/the-ringer-nba-show', podcasts_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports Videos (YouTube)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('JomBoy Media', 'jomboy', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCl9E4Zxa8RG-XqPBEgzZPKw', 'https://www.youtube.com/@JomboyMedia', videos_id, true),
        ('Secret Base', 'secret-base', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCDRmGMSgrtZkOsh_NQl4_xw', 'https://www.youtube.com/@SecretBaseSBN', videos_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business Podcasts
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('How I Built This', 'how-i-built-this', 'https://feeds.npr.org/510313/podcast.xml', 'https://www.npr.org/podcasts/510313/how-i-built-this', podcasts_id, true),
        ('Acquired', 'acquired', 'https://feeds.megaphone.fm/acquired', 'https://www.acquired.fm', podcasts_id, true),
        ('The All-In Podcast', 'all-in', 'https://feeds.megaphone.fm/all-in-with-chamath-jason-sacks-friedberg', 'https://www.allinpodcast.co', podcasts_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business Videos (YouTube)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('CNBC', 'cnbc', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCvJJ_dzjViJCoLf5uKUTwoA', 'https://www.youtube.com/@CNBC', videos_id, true),
        ('Bloomberg Television', 'bloomberg-tv', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCIALMKvObZNtJ6AmdCLP7Lg', 'https://www.youtube.com/@BloombergTelevision', videos_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment Podcasts
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('SmartLess', 'smartless', 'https://feeds.simplecast.com/hNAqTLMc', 'https://www.smartless.com', podcasts_id, true),
        ('Conan O''Brien Needs a Friend', 'conan-obrien', 'https://feeds.simplecast.com/dHoohVNH', 'https://www.earwolf.com/show/conan-obrien/', podcasts_id, true),
        ('Armchair Expert', 'armchair-expert', 'https://feeds.simplecast.com/5aZLc4X8', 'https://armchairexpertpod.com', podcasts_id, true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment Videos (YouTube)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
        ('First We Feast', 'first-we-feast', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCPD_bxCRGpmmeQcbe2kpPaA', 'https://www.youtube.com/@FirstWeFeast', videos_id, true),
        ('The Tonight Show', 'tonight-show', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC8-Th83bH_thdKZDJCrn88g', 'https://www.youtube.com/@TheTonightShow', videos_id, true),
        ('Hot Ones', 'hot-ones', 'https://www.youtube.com/feeds/videos.xml?playlist_id=PLAzrgbu8gEMIIK3r4Se1dOZWSZzUSadfZ', 'https://www.youtube.com/playlist?list=PLAzrgbu8gEMIIK3r4Se1dOZWSZzUSadfZ', videos_id, true)
    ON CONFLICT (slug) DO NOTHING;

END $$;
