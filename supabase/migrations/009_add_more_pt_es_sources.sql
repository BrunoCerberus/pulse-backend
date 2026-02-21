-- Migration: Add more Portuguese and Spanish RSS sources
-- Fills category gaps (health, entertainment) and strengthens thin categories

DO $$
DECLARE
    world_id UUID;
    business_id UUID;
    sports_id UUID;
    science_id UUID;
    entertainment_id UUID;
    health_id UUID;
BEGIN
    SELECT id INTO world_id FROM categories WHERE slug = 'world';
    SELECT id INTO business_id FROM categories WHERE slug = 'business';
    SELECT id INTO sports_id FROM categories WHERE slug = 'sports';
    SELECT id INTO science_id FROM categories WHERE slug = 'science';
    SELECT id INTO entertainment_id FROM categories WHERE slug = 'entertainment';
    SELECT id INTO health_id FROM categories WHERE slug = 'health';

    -- ==========================================================================
    -- Portuguese Sources (pt) — 10 new sources
    -- ==========================================================================

    -- Health (new category for pt)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Veja Saude', 'veja-saude', 'https://saude.abril.com.br/feed/', 'https://saude.abril.com.br', health_id, 'pt', true),
        ('Metropoles Saude', 'metropoles-saude', 'https://www.metropoles.com/saude/feed', 'https://www.metropoles.com/saude', health_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment (new category for pt)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('CinePOP', 'cinepop', 'https://cinepop.com.br/feed/', 'https://cinepop.com.br', entertainment_id, 'pt', true),
        ('PapelPop', 'papelpop', 'https://www.papelpop.com/feed/', 'https://www.papelpop.com', entertainment_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Gazeta Esportiva', 'gazeta-esportiva', 'https://www.gazetaesportiva.com/feed/', 'https://www.gazetaesportiva.com', sports_id, 'pt', true),
        ('UOL Esporte', 'uol-esporte', 'https://rss.uol.com.br/feed/esporte.xml', 'https://www.uol.com.br/esporte/', sports_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Revista Galileu', 'revista-galileu', 'https://revistagalileu.globo.com/rss/galileu', 'https://revistagalileu.globo.com', science_id, 'pt', true),
        ('Superinteressante', 'superinteressante', 'https://super.abril.com.br/feed/', 'https://super.abril.com.br', science_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Valor Economico', 'valor-economico', 'https://pox.globo.com/rss/valor/', 'https://valor.globo.com', business_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- World
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('UOL Noticias', 'uol-noticias', 'https://rss.uol.com.br/feed/noticias.xml', 'https://noticias.uol.com.br', world_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- ==========================================================================
    -- Spanish Sources (es) — 9 new sources
    -- ==========================================================================

    -- Health (new category for es)
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('20 Minutos Salud', '20-minutos-salud', 'https://www.20minutos.es/rss/salud/', 'https://www.20minutos.es/salud/', health_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Espinof', 'espinof', 'https://www.espinof.com/feedburner.xml', 'https://www.espinof.com', entertainment_id, 'es', true),
        ('20 Minutos Cine', '20-minutos-cine', 'https://www.20minutos.es/rss/cine/', 'https://www.20minutos.es/cine/', entertainment_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Mundo Deportivo', 'mundo-deportivo', 'https://www.mundodeportivo.com/rss/portada', 'https://www.mundodeportivo.com', sports_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('National Geographic Espana', 'natgeo-espana', 'https://www.nationalgeographic.com.es/feeds/rss', 'https://www.nationalgeographic.com.es', science_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Cinco Dias', 'cinco-dias', 'https://feeds.elpais.com/mrss-s/pages/ep/site/cincodias.elpais.com/portada', 'https://cincodias.elpais.com', business_id, 'es', true),
        ('El Economista', 'el-economista', 'https://www.eleconomista.es/rss/rss-seleccion-ee.xml', 'https://www.eleconomista.es', business_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- World
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Infobae', 'infobae', 'https://www.infobae.com/arc/outboundfeeds/rss/', 'https://www.infobae.com', world_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

END $$;
