-- Migration: Add Portuguese and Spanish podcasts, videos, and politics sources
-- Adds 46 new sources: 10 PT podcasts, 10 PT videos, 3 PT politics,
--                       10 ES podcasts, 10 ES videos, 3 ES politics

DO $$
DECLARE
    podcasts_id UUID;
    videos_id UUID;
    politics_id UUID;
BEGIN
    SELECT id INTO podcasts_id FROM categories WHERE slug = 'podcasts';
    SELECT id INTO videos_id FROM categories WHERE slug = 'videos';
    SELECT id INTO politics_id FROM categories WHERE slug = 'politics';

    -- ==========================================================================
    -- Portuguese Podcasts (pt) — 10 new sources
    -- ==========================================================================

    -- Entertainment / Tech
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('NerdCast', 'nerdcast', 'https://feeds.megaphone.fm/JNPD6227286900', 'https://jovemnerd.com.br/nerdcast/', podcasts_id, 'pt', true),
        ('Flow Podcast', 'flow-podcast', 'https://anchor.fm/s/a5637400/podcast/rss', 'https://www.youtube.com/@FlowPodcast', podcasts_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- News
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Cafe da Manha', 'cafe-da-manha', 'https://anchor.fm/s/21d31ba4/podcast/rss', 'https://www.folha.uol.com.br/podcasts/cafe-da-manha/', podcasts_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Technology
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Braincast', 'braincast', 'https://www.omnycontent.com/d/playlist/651a251e-06e1-47e0-9336-ac5a00f41628/fc243b66-f34c-4656-9042-acd400edcca5/d4c8e398-446c-447a-ad41-acd400edccc1/podcast.rss', 'https://www.b9.com.br/shows/braincast/', podcasts_id, 'pt', true),
        ('Hipsters Ponto Tech', 'hipsters-ponto-tech', 'https://www.hipsters.tech/feed/podcast/', 'https://www.hipsters.tech', podcasts_id, 'pt', true),
        ('Tecnocast', 'tecnocast', 'https://anchor.fm/s/1075f6ce0/podcast/rss', 'https://tecnoblog.net/tecnocast/', podcasts_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Naruhodo', 'naruhodo', 'https://feeds.simplecast.com/hwQVm5gy', 'https://www.b9.com.br/shows/naruhodo/', podcasts_id, 'pt', true),
        ('Dragoes de Garagem', 'dragoes-de-garagem', 'https://dragoesdegaragem.com/feed/podcast/', 'https://dragoesdegaragem.com', podcasts_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('PrimoCast', 'primocast', 'https://anchor.fm/s/46a7ee28/podcast/rss', 'https://www.youtube.com/@primo-rico', podcasts_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Politics
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Xadrez Verbal', 'xadrez-verbal', 'https://www.spreaker.com/show/4712237/episodes/feed', 'https://xadrezverbal.com', podcasts_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- ==========================================================================
    -- Portuguese Videos (pt) — 10 new YouTube channels
    -- ==========================================================================

    -- Technology
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('TecMundo', 'tecmundo-yt', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCdmGjywrxeOPfC7vDllmSgQ', 'https://www.youtube.com/@TecMundo', videos_id, 'pt', true),
        ('Filipe Deschamps', 'filipe-deschamps', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCU5JicSrEM5A63jkJ2QvGYw', 'https://www.youtube.com/@FilipeDeschamps', videos_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Manual do Mundo', 'manual-do-mundo', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCKHhA5hN2UohhFDfNXB_cvQ', 'https://www.youtube.com/@manualdomundo', videos_id, 'pt', true),
        ('Nerdologia', 'nerdologia', 'https://www.youtube.com/feeds/videos.xml?channel_id=UClu474HMt895mVxZdlIHXEA', 'https://www.youtube.com/@naborfranca', videos_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- News
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('BBC News Brasil', 'bbc-news-brasil-yt', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCthbIFAxbXTTQEC7EcQvP1Q', 'https://www.youtube.com/@bbcnewsbrasil', videos_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Desimpedidos', 'desimpedidos', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCFjrDmEnxrG5TRGVO0TPHLA', 'https://www.youtube.com/@Desimpedidos', videos_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Porta dos Fundos', 'porta-dos-fundos', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCEWHPFNilsT0IfQfutVzsag', 'https://www.youtube.com/@PortadosFundos', videos_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Me Poupe!', 'me-poupe', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC8mDF5mWNGE-Kpfcvnn0bUg', 'https://www.youtube.com/@MePoupe', videos_id, 'pt', true),
        ('O Primo Rico', 'o-primo-rico', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCT4nDeU5pv1XIGySbSK-GgA', 'https://www.youtube.com/@OPrimoRico', videos_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Health
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Drauzio Varella', 'drauzio-varella', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC9zqTTVeClpqMQ5CLuJdWtw', 'https://www.youtube.com/@draaborges', videos_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- ==========================================================================
    -- Portuguese Politics Articles (pt) — 3 new sources
    -- ==========================================================================

    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Poder360', 'poder360', 'https://www.poder360.com.br/feed/', 'https://www.poder360.com.br', politics_id, 'pt', true),
        ('Congresso em Foco', 'congresso-em-foco', 'https://www.congressoemfoco.com.br/feed/', 'https://www.congressoemfoco.com.br', politics_id, 'pt', true),
        ('Folha de S.Paulo Poder', 'folha-poder', 'https://feeds.folha.uol.com.br/poder/rss091.xml', 'https://www.folha.uol.com.br/poder/', politics_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- ==========================================================================
    -- Spanish Podcasts (es) — 10 new sources
    -- ==========================================================================

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('TED en Espanol', 'ted-en-espanol', 'https://feeds.acast.com/public/shows/c7c8d4c1-dcdb-4ed7-95e9-1aade928b5f9', 'https://www.ted.com/podcasts/ted-en-espanol', podcasts_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- News
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Radio Ambulante', 'radio-ambulante', 'https://www.omnycontent.com/d/playlist/e73c998e-6e60-432f-8610-ae210140c5b1/b3c9b6e7-72ba-45c4-aff9-b1e7012d213b/092b66a8-4329-4183-bb12-b1e7012d216f/podcast.rss', 'https://radioambulante.org', podcasts_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Health
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Entiende Tu Mente', 'entiende-tu-mente', 'https://feeds.acast.com/public/shows/entiende-tu-mente-etm', 'https://entiendetumente.info', podcasts_id, 'es', true),
        ('Cristina Mitre', 'cristina-mitre', 'https://feeds.acast.com/public/shows/62ed09b7dd4e730012475bd3', 'https://thebeautymail.es', podcasts_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Se Regalan Dudas', 'se-regalan-dudas', 'https://feeds.acast.com/public/shows/se-regalan-dudas', 'https://www.seregalandudas.com', podcasts_id, 'es', true),
        ('Nadie Sabe Nada', 'nadie-sabe-nada', 'https://www.spreaker.com/show/6040245/episodes/feed', 'https://cadenaser.com/programa/nadie_sabe_nada/', podcasts_id, 'es', true),
        ('The Wild Project', 'the-wild-project', 'https://feeds.megaphone.fm/TWIP9771253765', 'https://www.youtube.com/@TheWildProject', podcasts_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Technology
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Despeja la X', 'despeja-la-x', 'https://feeds.ivoox.com/feed_fg_f1579492_filtro_1.xml', 'https://elpais.com/podcast/despeja-la-x/', podcasts_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('El Partidazo de COPE', 'el-partidazo-cope', 'https://www.cope.es/api/es/programas/el-partidazo-de-cope/audios/rss.xml', 'https://www.cope.es/programas/el-partidazo-de-cope/', podcasts_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('BBVA Blink', 'bbva-blink', 'https://rss.libsyn.com/shows/148109/destinations/948086.xml', 'https://www.bbva.com/es/podcast/', podcasts_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- ==========================================================================
    -- Spanish Videos (es) — 10 new YouTube channels
    -- ==========================================================================

    -- Technology
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Nate Gentile', 'nate-gentile', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC36xmz34q02JYaZYKrMwXng', 'https://www.youtube.com/@NateGentile7', videos_id, 'es', true),
        ('Dot CSV', 'dot-csv', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCy5znSnfMsDwaLlROnZ7Qbg', 'https://www.youtube.com/@DotCSV', videos_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('QuantumFracture', 'quantumfracture', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCbdSYaPD-lr1kW27UJuk8Pw', 'https://www.youtube.com/@QuantumFracture', videos_id, 'es', true),
        ('CdeCiencia', 'cdeciencia', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC52hytXteCKmuOzMViTK8_w', 'https://www.youtube.com/@CdeCiencia', videos_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- News
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('BBC News Mundo', 'bbc-news-mundo-yt', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCUBIrDsIVzRpKsClMlSlTpQ', 'https://www.youtube.com/@bbcmundo', videos_id, 'es', true),
        ('DW Espanol', 'dw-espanol', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCT4Jg8h03dD0iN3Pb5L0PMA', 'https://www.youtube.com/@DWEspanol', videos_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Ibai', 'ibai', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCaY_-ksFSQtTGk0y1HA_3YQ', 'https://www.youtube.com/@IbaiLlanos', videos_id, 'es', true),
        ('Luisito Comunica', 'luisito-comunica', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCECJDeK0MNapZbpaOzxrUPA', 'https://www.youtube.com/@LuisitoComunica', videos_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Value School', 'value-school', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCpLie5obXFdf8T0NG-IRHsA', 'https://www.youtube.com/@ValueSchool', videos_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Health
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('FisioOnline', 'fisioonline', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC6iRiXWScChTr6uNLXjJYFQ', 'https://www.youtube.com/@FisioOnline', videos_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- ==========================================================================
    -- Spanish Politics Articles (es) — 3 new sources
    -- ==========================================================================

    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('elDiario.es Politica', 'eldiario-politica', 'https://www.eldiario.es/rss/politica/', 'https://www.eldiario.es/politica/', politics_id, 'es', true),
        ('La Vanguardia Politica', 'la-vanguardia-politica', 'https://www.lavanguardia.com/rss/politica.xml', 'https://www.lavanguardia.com/politica', politics_id, 'es', true),
        ('El Confidencial', 'el-confidencial', 'https://rss.elconfidencial.com/espana/', 'https://www.elconfidencial.com/espana/', politics_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

END $$;
